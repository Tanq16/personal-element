package daemon

import "fmt"

const promptTemplate = `You have been requested to act on this message:

%s

Write your final answer to %s. That file is a throwaway, so overwrite it whole and
ignore whatever it already holds. It is the only thing sent back, so it has to carry
the complete answer on its own.`

const retrievalTemplate = `

If the message does not carry enough context on its own, work out from it what you
still need, then read the conversation before it in batches of %d messages. Repeat the
command until you have what the request needs.

curl -sS -H "Authorization: Bearer $ELEMENT_AGENT_TOKEN" %s`

func composePrompt(agent Agent, dir, body string, limit int, loopback string) string {
	prompt := fmt.Sprintf(promptTemplate, body, resultPath(dir))
	if agent.AllowMessageRetrieval {
		prompt += fmt.Sprintf(retrievalTemplate, limit, "http://"+loopback+"/context")
	}
	return prompt
}
