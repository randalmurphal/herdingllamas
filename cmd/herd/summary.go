package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/randalmurphal/herdingllamas/internal/debate"
	"github.com/randalmurphal/herdingllamas/internal/store"
	"github.com/randalmurphal/llmkit/claude"
)

// summaryJSON is the JSON output format for the summary command.
type summaryJSON struct {
	DebateID     string   `json:"debate_id"`
	Question     string   `json:"question"`
	Status       string   `json:"status"`
	Participants []string `json:"participants"`
	TurnCount    int      `json:"turn_count"`
	Duration     string   `json:"duration"`
	Summary      string   `json:"summary"`
}

func summaryCmd() *cobra.Command {
	var debateID string
	var latest bool
	var jsonOutput bool

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
			if debate == nil {
				return fmt.Errorf("debate not found: %s", debateID)
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

			shortID := debateID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			fmt.Fprintf(os.Stderr, "Summarizing debate %s (%d messages)...\n", shortID, len(messages))

			// Call Claude to produce the summary, using the mode-appropriate prompt.
			mode := extractMode(debate)
			summary, err := generateSummary(ctx, debate, transcript, mode)
			if err != nil {
				return fmt.Errorf("generating summary: %w", err)
			}

			if jsonOutput {
				participants, turnCount := analyzeMessages(messages)
				var duration time.Duration
				if debate.EndedAt != nil {
					duration = debate.EndedAt.Sub(debate.CreatedAt)
				}
				out := summaryJSON{
					DebateID:     debate.ID,
					Question:     debate.Question,
					Status:       debate.Status,
					Participants: participants,
					TurnCount:    turnCount,
					Duration:     formatDuration(duration),
					Summary:      summary,
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			fmt.Println(summary)
			return nil
		},
	}

	cmd.Flags().StringVar(&debateID, "debate", "", "Debate ID to summarize")
	cmd.Flags().BoolVar(&latest, "latest", false, "Summarize the most recent session")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output summary as JSON")

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

const interrogationSummarySystemPrompt = `You are synthesizing an Advocate/Interrogator plan interrogation transcript into a structured plan review.

Your job is to extract the findings from the session and organize them into a deliverable a developer can act on immediately. You are not adding your own analysis — the agents had full tool access, read the code, and did the research. You are structuring their findings clearly and completely so nothing gets lost in the conversation format.

Structure your response as:

## Plan Assessment
A direct verdict on the plan's readiness for implementation based on what the session found. 2-3 paragraphs covering: is this plan ready to build? What's the overall quality? What's the single biggest risk that was identified?

## Confirmed Strengths
Design decisions that were challenged during interrogation and held up with evidence. Include the evidence — the developer needs to know these are validated, not just unchallenged.

## Identified Gaps
Every gap surfaced during the session. For each gap:
- **What**: The specific problem
- **Evidence**: How it was discovered (code reference, research finding, traced data path)
- **Severity**: Blocker (must fix before implementation), Risk (must address during implementation), or Improvement (would strengthen the plan)
- **Recommended fix**: How the plan should be amended (use the agents' proposed fixes where they exist)

Order by severity — blockers first. Include gaps the Advocate proactively surfaced, gaps the Interrogator found, and any points the Interrogator raised that the Advocate couldn't adequately defend — even if not explicitly confirmed as gaps.

## Uncovered Dimensions
If any dimensions from the interrogation checklist were not adequately explored, list them here. Absence of investigation is not the same as absence of problems.

## Implementation Recommendations
Based on everything surfaced, what should the developer do next? Prioritized, specific, actionable. Drawn from the session's findings, not generic advice.

Rules:
- The developer needs to know what to FIX, not what was DISCUSSED.
- If the Advocate confirmed a gap, that's the highest-confidence signal — lead with those.
- If the Advocate proactively surfaced a concern in their opening analysis, that's high-signal — they found it during their deepest read.
- Be complete. Comb the full transcript — findings often emerge mid-exchange and are easy to miss in the back-and-forth.
- If the interrogation was shallow on any dimension, say so — a clean bill of health from a shallow review is worse than no review.
- Don't pad. If the plan is solid, say so briefly. If it's full of gaps, be thorough.`

const refineSummarySystemPrompt = `You are synthesizing an Evaluator/Refiner prompt refinement session into an actionable improvement report.

Your job is to extract every concrete change from the session and organize them so the prompt author can apply them immediately. You are not adding your own analysis — the agents evaluated the prompt in depth. You are structuring their findings clearly and completely.

Structure your response as:

## Prompt Assessment
A direct verdict on the prompt's quality. 2-3 paragraphs covering: how effective is this prompt currently? What's the single biggest improvement opportunity? Is this a minor polish or a significant rework?

## Accepted Changes
Every change the Refiner accepted. For each:
- **Finding**: What the Evaluator identified (with quote from original)
- **Change**: The exact before/after text replacement
- **Rationale**: Why this improves the prompt's effectiveness

Order by impact — most impactful changes first.

## Defended Choices
Design decisions the Evaluator challenged that the Refiner successfully defended. Include the defense reasoning — these are intentional patterns that should be preserved.

## Remaining Concerns
Findings that were not fully resolved. Include both sides so the prompt author can decide.

## Revised Prompt
If the session produced enough accepted changes to warrant it, present the full revised prompt with all accepted changes applied in a fenced code block.

Rules:
- The prompt author needs copy-paste-ready changes, not a discussion about prompting theory.
- Include EXACT text replacements — before and after. Vague suggestions are useless.
- If the session was shallow or produced few meaningful improvements, say so honestly.
- Preserve findings from the full transcript — changes often emerge mid-exchange.
- If the agents disagreed on a finding, present both arguments and let the author decide.`

// generateSummary calls Claude to produce an evaluated summary of the debate.
// The system prompt varies by mode to produce the most useful output format.
func generateSummary(ctx context.Context, d *store.Debate, transcript string, mode debate.Mode) (string, error) {
	client := claude.NewClaudeCLI(
		claude.WithDangerouslySkipPermissions(),
	)

	sysPrompt := summarySystemPrompt
	if mode == debate.ModeInterrogate {
		sysPrompt = interrogationSummarySystemPrompt
	}
	if mode == debate.ModeRefinePrompt {
		sysPrompt = refineSummarySystemPrompt
	}

	userMessage := fmt.Sprintf("Here is the full transcript of a multi-agent session. Read it carefully and produce your evaluated summary.\n\n%s", transcript)

	resp, err := client.Complete(ctx, claude.CompletionRequest{
		SystemPrompt: sysPrompt,
		Messages: []claude.Message{
			{Role: claude.RoleUser, Content: userMessage},
		},
	})
	if err != nil {
		return "", fmt.Errorf("claude completion: %w", err)
	}

	return resp.Content, nil
}

// extractMode parses the debate mode from the stored config JSON.
// Returns ModeDebate as a default if the config can't be parsed.
func extractMode(d *store.Debate) debate.Mode {
	if d.Config == "" {
		return debate.ModeDebate
	}
	var cfg struct {
		Mode debate.Mode `json:"Mode"`
	}
	if err := json.Unmarshal([]byte(d.Config), &cfg); err != nil {
		return debate.ModeDebate
	}
	if cfg.Mode == "" {
		return debate.ModeDebate
	}
	return cfg.Mode
}

// analyzeMessages extracts unique participants (excluding moderator/system) and
// counts the number of agent turns.
func analyzeMessages(messages []store.Message) (participants []string, turnCount int) {
	seen := make(map[string]bool)
	for _, msg := range messages {
		if msg.Author == "system" || msg.Author == "moderator" {
			continue
		}
		if !seen[msg.Author] {
			seen[msg.Author] = true
			participants = append(participants, msg.Author)
		}
		turnCount++
	}
	sort.Strings(participants)
	return participants, turnCount
}

// formatDuration formats a duration as MM:SS or HH:MM:SS.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
