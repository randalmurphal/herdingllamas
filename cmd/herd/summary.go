package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/randalmurphal/herdingllamas/internal/store"
	"github.com/randalmurphal/llmkit/claude"
)

func summaryCmd() *cobra.Command {
	var debateID string
	var latest bool

	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Produce an evaluated summary of a debate or exploration",
		Long: `Reads a completed session transcript and produces a structured summary
using a Claude agent that evaluates the arguments, extracts key findings,
and answers the original question based on the full discussion.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			dbPath, err := store.DefaultDBPath()
			if err != nil {
				return fmt.Errorf("resolving DB path: %w", err)
			}

			st, err := store.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer st.Close()

			// Resolve the debate ID.
			if latest {
				debates, err := st.ListDebates()
				if err != nil {
					return fmt.Errorf("listing debates: %w", err)
				}
				if len(debates) == 0 {
					return fmt.Errorf("no debates found")
				}
				debateID = debates[0].ID
			}
			if debateID == "" {
				return fmt.Errorf("provide --debate <id> or --latest")
			}

			debate, err := st.GetDebate(debateID)
			if err != nil {
				return fmt.Errorf("getting debate: %w", err)
			}

			messages, err := st.GetDebateMessages(debateID)
			if err != nil {
				return fmt.Errorf("getting messages: %w", err)
			}
			if len(messages) == 0 {
				return fmt.Errorf("no messages found for debate %s", debateID)
			}

			// Build the transcript for the summary agent.
			transcript := formatTranscript(debate, messages)

			fmt.Fprintf(os.Stderr, "Summarizing debate %s (%d messages)...\n", debateID[:8], len(messages))

			// Call Claude to produce the summary.
			summary, err := generateSummary(ctx, debate, transcript)
			if err != nil {
				return fmt.Errorf("generating summary: %w", err)
			}

			fmt.Println(summary)
			return nil
		},
	}

	cmd.Flags().StringVar(&debateID, "debate", "", "Debate ID to summarize")
	cmd.Flags().BoolVar(&latest, "latest", false, "Summarize the most recent session")

	return cmd
}

// formatTranscript builds a readable transcript from debate messages.
func formatTranscript(debate *store.Debate, messages []store.Message) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("QUESTION/TOPIC: %s\n", debate.Question))
	b.WriteString(fmt.Sprintf("STATUS: %s\n", debate.Status))
	b.WriteString(fmt.Sprintf("STARTED: %s\n", debate.CreatedAt.Format(time.RFC3339)))
	if debate.EndedAt != nil {
		b.WriteString(fmt.Sprintf("ENDED: %s\n", debate.EndedAt.Format(time.RFC3339)))
	}
	b.WriteString(fmt.Sprintf("TOTAL MESSAGES: %d\n", len(messages)))
	b.WriteString("\n--- TRANSCRIPT ---\n\n")

	for _, msg := range messages {
		// Skip system conclude notifications — they're noise for the summarizer.
		if msg.Author == "system" {
			continue
		}
		b.WriteString(fmt.Sprintf("[Turn %d] %s (%s):\n%s\n\n",
			msg.TurnNum, msg.Author, msg.Timestamp.Format("15:04:05"), msg.Content))
	}

	return b.String()
}

const summarySystemPrompt = `You are an expert analyst evaluating a multi-agent discussion transcript.

Your job is to read the full transcript of a conversation between AI agents and produce a response that is USEFUL TO THE DEVELOPER WHO ASKED THE ORIGINAL QUESTION.

You are not summarizing what happened in the conversation. You are producing the ANSWER to the original question, informed by the best evidence and arguments from both agents.

Structure your response as:

## Answer
A direct, concise answer to the original question. 2-4 paragraphs that give the developer what they need to know. Use the strongest evidence and arguments from the discussion, but write it as YOUR synthesis — not as a report about what the agents said.

## Key Findings
Bullet points of the most important discoveries, insights, or conclusions from the discussion. Only include things that are genuinely useful — not obvious observations.

## Open Questions
Things the discussion surfaced but did not resolve. These are the questions the developer should think about next.

## Strongest Evidence
The most compelling specific citations, data points, research findings, or examples that came up. Include enough context that these are useful standalone.

Rules:
- Be direct. The developer wants the answer, not a meta-analysis of the conversation.
- If the agents converged on a safe middle ground, say what the INTERESTING disagreements were before they converged.
- If one agent made a strong point that the other conceded, that point is probably important.
- If both agents missed something obvious, point it out.
- Don't pad. If the discussion was thin, say so.`

// generateSummary calls Claude to produce an evaluated summary of the debate.
func generateSummary(ctx context.Context, debate *store.Debate, transcript string) (string, error) {
	client := claude.NewClaudeCLI(
		claude.WithDangerouslySkipPermissions(),
		claude.WithMaxTurns(1),
	)

	userMessage := fmt.Sprintf("Here is the full transcript of a multi-agent session. Read it carefully and produce your evaluated summary.\n\n%s", transcript)

	resp, err := client.Complete(ctx, claude.CompletionRequest{
		SystemPrompt: summarySystemPrompt,
		Messages: []claude.Message{
			{Role: claude.RoleUser, Content: userMessage},
		},
	})
	if err != nil {
		return "", fmt.Errorf("claude completion: %w", err)
	}

	return resp.Content, nil
}
