package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
)

// Every sentence the server puts in front of the model is in this file,
// so what the model reads can be read in one place. The rest comes from
// the agent's instructions, the runtime's harness, and the API's answers.

// consoleConventions follow every console agent's instructions: how the
// console shows an answer. They are the console's contract with the
// model, so they live here rather than in instructions a member edits,
// and read the same on every turn, for the prompt cache.
const consoleConventions = "<console>\n" +
	"Your answers show in the ShopNexus console, which renders Markdown: tables, diagrams in ```mermaid blocks, and charts as Vega-Lite JSON in ```vega-lite blocks, data inline and no styling: the console styles them.\n" +
	"Write a record as a link, [its name](kind:id), kind being account, ticket, order, listing or report; the console draws it as a chip that opens the record.\n" +
	"When the user has to choose between a few replies, end your answer with an ```options block, one reply per line; the console shows them as buttons, and the one picked comes back as the user's next message.\n" +
	"</console>"

// instructionsOf is the system prompt the server sends for agent a; the
// runtime adds its harness after it.
func instructionsOf(a repository.Agent) string {
	return strings.TrimSpace(a.Instructions) + "\n\n" + consoleConventions
}

// member names a staff member to the model.
func member(name, role, id string) string {
	return fmt.Sprintf("%s (%s, id %s)", name, role, id)
}

// factsText is the context of a member's message. The member and the
// record are said once, when the conversation starts or another member
// writes; the history keeps them for later messages.
func factsText(now time.Time, who string, newWriter bool, subject, decided string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Time: %s\n", now.Format("2006-01-02 15:04 MST"))
	if newWriter {
		fmt.Fprintf(&b, "Staff member: %s\n", who)
	}
	if subject != "" {
		fmt.Fprintf(&b, "This conversation is about %s.\n", subject)
	}
	b.WriteString(decided)
	return strings.TrimSpace(b.String())
}

// steerText is the context of a steer: a message written while the turn
// ran. Its writer is named only when it is not the turn's member.
func steerText(other string) string {
	if other == "" {
		return "Written while you worked."
	}
	return "Written while you worked, by " + other + "."
}

// decisionsText tells the agent what became of the changes it asked for.
// An approved change succeeded; a failed one says how its call answered.
func decisionsText(list []repository.Approval) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Changes you asked for, decided since:\n")
	for i := range list {
		a := &list[i]
		fmt.Fprintf(&b, "- %s %s: %s", a.Operation, a.Path, a.Status)
		if a.Status == domain.ApprovalFailed && a.ResultStatus != nil {
			fmt.Fprintf(&b, ", the call answered %d", *a.ResultStatus)
			if len(a.ResultBody) > 0 {
				fmt.Fprintf(&b, " %s", cut(string(a.ResultBody), 300))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
