package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/randalmurphal/herdingllamas/internal/store"
)

func channelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Debate channel operations",
		Long:  "Commands for posting to and reading from a debate channel. Used by LLM agents during debates.",
	}

	cmd.AddCommand(channelPostCmd())
	cmd.AddCommand(channelReadCmd())
	cmd.AddCommand(channelWaitCmd())
	cmd.AddCommand(channelConcludeCmd())

	return cmd
}

func channelPostCmd() *cobra.Command {
	var debateID, from string

	cmd := &cobra.Command{
		Use:   "post [message]",
		Short: "Post a message to the debate channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := store.DefaultDBPath()
			if err != nil {
				return fmt.Errorf("resolving DB path: %w", err)
			}

			st, err := store.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer st.Close()

			// Verify the debate exists and is active.
			debate, err := st.GetDebate(debateID)
			if err != nil {
				return fmt.Errorf("looking up debate: %w", err)
			}
			if debate == nil {
				return fmt.Errorf("debate %s not found", debateID)
			}
			if debate.Status != "active" {
				return fmt.Errorf("debate %s is %s, not active", debateID, debate.Status)
			}

			msg, err := st.PostMessage(debateID, from, args[0])
			if err != nil {
				return fmt.Errorf("posting message: %w", err)
			}

			// Do NOT advance the cursor here. The cursor should only advance
			// when the agent explicitly reads via `herd channel read`. The
			// nudge system already excludes self-messages (GetUnreadCount
			// filters author != agentName), so the agent won't be nudged
			// about their own posts.

			// If this agent had previously concluded, posting a new message
			// revokes that conclusion — they clearly have more to say.
			if err := st.RevokeConcluded(debateID, from); err != nil {
				return fmt.Errorf("revoking conclusion: %w", err)
			}

			fmt.Printf("Posted (turn %d).\n", msg.TurnNum)

			statuses, err := st.GetAgentStatuses(debateID)
			if err != nil {
				return fmt.Errorf("getting agent statuses: %w", err)
			}
			statusLines := formatOtherAgentStatuses(statuses, from)
			if statusLines != "" {
				fmt.Printf("Status: %s\n", statusLines)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&debateID, "debate", "", "Debate ID (required)")
	cmd.Flags().StringVar(&from, "from", "", "Agent name posting the message (required)")
	cmd.MarkFlagRequired("debate")
	cmd.MarkFlagRequired("from")

	return cmd
}

func channelReadCmd() *cobra.Command {
	var debateID, agentName string

	cmd := &cobra.Command{
		Use:   "read",
		Short: "Read new messages from the debate channel",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := store.DefaultDBPath()
			if err != nil {
				return fmt.Errorf("resolving DB path: %w", err)
			}

			st, err := store.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer st.Close()

			messages, err := st.GetUnreadMessages(debateID, agentName)
			if err != nil {
				return fmt.Errorf("reading messages: %w", err)
			}

			if len(messages) == 0 {
				fmt.Println("No new messages.")
				return nil
			}

			fmt.Printf("--- %d new message(s) ---\n\n", len(messages))

			for _, m := range messages {
				fmt.Printf("[Turn %d] %s (%s):\n%s\n\n",
					m.TurnNum, m.Author, m.Timestamp.Format("15:04:05"), m.Content)
			}

			fmt.Println("--- End of messages ---")

			// Advance cursor to the latest message.
			lastTurn := messages[len(messages)-1].TurnNum
			if err := st.UpdateCursor(debateID, agentName, lastTurn); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			statuses, err := st.GetAgentStatuses(debateID)
			if err != nil {
				return fmt.Errorf("getting agent statuses: %w", err)
			}
			statusLines := formatOtherAgentStatuses(statuses, agentName)
			if statusLines != "" {
				fmt.Printf("Status: %s\n", statusLines)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&debateID, "debate", "", "Debate ID (required)")
	cmd.Flags().StringVar(&agentName, "agent", "", "Agent name reading messages (required)")
	cmd.MarkFlagRequired("debate")
	cmd.MarkFlagRequired("agent")

	return cmd
}

func channelWaitCmd() *cobra.Command {
	var debateID, agentName string
	var timeoutSecs int

	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Wait for the other participant to respond",
		Long:  "Blocks until the other participant reads and responds, a conclusion is proposed, or the timeout is reached.",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := store.DefaultDBPath()
			if err != nil {
				return fmt.Errorf("resolving DB path: %w", err)
			}

			st, err := store.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer st.Close()

			// Verify the debate exists and is active.
			debate, err := st.GetDebate(debateID)
			if err != nil {
				return fmt.Errorf("looking up debate: %w", err)
			}
			if debate == nil {
				return fmt.Errorf("debate %s not found", debateID)
			}
			if debate.Status != "active" {
				fmt.Printf("Debate is %s.\n", debate.Status)
				return nil
			}

			// If the agent has unread messages, tell them to read first.
			unread, err := st.GetUnreadCount(debateID, agentName)
			if err != nil {
				return fmt.Errorf("checking unread count: %w", err)
			}
			if unread > 0 {
				fmt.Printf("You have %d unread message(s). Read them before waiting.\n", unread)
				return nil
			}

			// Record baseline state.
			baselineTurn, err := st.GetLatestTurnNum(debateID)
			if err != nil {
				return fmt.Errorf("getting latest turn: %w", err)
			}

			// Determine our last post turn for cursor tracking.
			myLastPost := -1
			statuses, err := st.GetAgentStatuses(debateID)
			if err != nil {
				return fmt.Errorf("getting agent statuses: %w", err)
			}
			for _, s := range statuses {
				if s.Name == agentName {
					myLastPost = s.LastPostTurn
					break
				}
			}

			// Record baseline conclusion count.
			baselineConcluded, err := st.GetConcluded(debateID)
			if err != nil {
				return fmt.Errorf("getting conclusions: %w", err)
			}
			baselineConcludedSet := make(map[string]bool, len(baselineConcluded))
			for _, name := range baselineConcluded {
				baselineConcludedSet[name] = true
			}

			deadline := time.After(time.Duration(timeoutSecs) * time.Second)
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-deadline:
					fmt.Printf("No activity within %d seconds.\n", timeoutSecs)
					return nil

				case <-ticker.C:
					// Check if the debate ended while waiting.
					d, err := st.GetDebate(debateID)
					if err != nil {
						return fmt.Errorf("checking debate status: %w", err)
					}
					if d == nil || d.Status != "active" {
						fmt.Println("Debate has ended.")
						return nil
					}

					// Check for new conclusion from another agent.
					concluded, err := st.GetConcluded(debateID)
					if err != nil {
						return fmt.Errorf("checking conclusions: %w", err)
					}
					for _, name := range concluded {
						if name != agentName && !baselineConcludedSet[name] {
							fmt.Printf("%s has proposed concluding the debate.\n", name)
							return nil
						}
					}

					// Check if another agent's cursor advanced past our last post.
					cursorAdvanced := false
					var advancedAgent string
					if myLastPost >= 0 {
						advancedAgent, cursorAdvanced, err = st.HasCursorAdvancedPast(debateID, agentName, myLastPost)
						if err != nil {
							return fmt.Errorf("checking cursor advancement: %w", err)
						}
					}

					// Check for new messages from other agents.
					newMsgs, err := st.GetMessagesAfterTurn(debateID, baselineTurn, agentName)
					if err != nil {
						return fmt.Errorf("checking for new messages: %w", err)
					}

					if len(newMsgs) > 0 && cursorAdvanced {
						lastNewMsg := newMsgs[len(newMsgs)-1]
						fmt.Printf("%s responded (turn %d) after reading your message.\n",
							lastNewMsg.Author, lastNewMsg.TurnNum)
						return nil
					}
					if cursorAdvanced && len(newMsgs) == 0 {
						fmt.Printf("%s read your message (turn %d) but hasn't responded yet.\n",
							advancedAgent, myLastPost)
						return nil
					}
					if len(newMsgs) > 0 && !cursorAdvanced {
						lastNewMsg := newMsgs[len(newMsgs)-1]
						fmt.Printf("%s posted (turn %d) but hasn't read your latest message yet.\n",
							lastNewMsg.Author, lastNewMsg.TurnNum)
						return nil
					}
				}
			}
		},
	}

	cmd.Flags().StringVar(&debateID, "debate", "", "Debate ID (required)")
	cmd.Flags().StringVar(&agentName, "agent", "", "The waiting agent's name (required)")
	cmd.Flags().IntVar(&timeoutSecs, "timeout", 90, "Seconds to wait before giving up")
	cmd.MarkFlagRequired("debate")
	cmd.MarkFlagRequired("agent")

	return cmd
}

// formatOtherAgentStatuses formats a status line showing all agents' status
// except the given agent. Returns an empty string if there are no other agents.
func formatOtherAgentStatuses(statuses []store.AgentStatus, excludeAgent string) string {
	var parts []string
	for _, s := range statuses {
		if s.Name == excludeAgent {
			continue
		}
		var fragments []string
		if s.LastReadTurn >= 0 {
			fragments = append(fragments, fmt.Sprintf("has read through turn %d", s.LastReadTurn))
		}
		if s.LastPostTurn >= 0 {
			fragments = append(fragments, fmt.Sprintf("last posted at turn %d", s.LastPostTurn))
		}
		if len(fragments) == 0 {
			fragments = append(fragments, "no activity yet")
		}
		parts = append(parts, fmt.Sprintf("%s %s", s.Name, strings.Join(fragments, ", ")))
	}
	return strings.Join(parts, "; ")
}

func channelConcludeCmd() *cobra.Command {
	var debateID, from string

	cmd := &cobra.Command{
		Use:   "conclude",
		Short: "Propose ending the debate by mutual agreement",
		Long:  "Records a conclusion vote. When all participants have concluded, the debate ends. Posting a new message after concluding revokes your vote.",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := store.DefaultDBPath()
			if err != nil {
				return fmt.Errorf("resolving DB path: %w", err)
			}

			st, err := store.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer st.Close()

			// Verify the debate exists and is active.
			debate, err := st.GetDebate(debateID)
			if err != nil {
				return fmt.Errorf("looking up debate: %w", err)
			}
			if debate == nil {
				return fmt.Errorf("debate %s not found", debateID)
			}
			if debate.Status != "active" {
				return fmt.Errorf("debate %s is %s, not active", debateID, debate.Status)
			}

			if err := st.SetConcluded(debateID, from); err != nil {
				return fmt.Errorf("recording conclusion: %w", err)
			}

			// Post a system message so the other agent sees the proposal
			// when they read the channel.
			sysMsg := fmt.Sprintf(
				"[SYSTEM: %s has proposed ending the debate. If you agree, run the conclude command. Otherwise, continue posting your arguments.]",
				from,
			)
			if _, err := st.PostMessage(debateID, "system", sysMsg); err != nil {
				return fmt.Errorf("posting conclusion notification: %w", err)
			}

			fmt.Println("Conclusion recorded. Waiting for other participant(s) to agree.")
			return nil
		},
	}

	cmd.Flags().StringVar(&debateID, "debate", "", "Debate ID (required)")
	cmd.Flags().StringVar(&from, "from", "", "Agent name proposing conclusion (required)")
	cmd.MarkFlagRequired("debate")
	cmd.MarkFlagRequired("from")

	return cmd
}
