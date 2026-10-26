package ai

import (
	"fmt"
	"strings"
	"time"
)

const voice = "You help with email for a busy support operations manager. Write in plain, direct English. No corporate jargon, no filler, no em dashes. Never invent facts that aren't in the material you're given. Output only the requested text, with no preamble, no labels and no explanation."

// The reader and compose windows hand the model plain text. Long emails
// are cut so a local model doesn't choke.
const maxChars = 24000

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxChars {
		return s[:maxChars] + "\n[... cut for length ...]"
	}
	return s
}

// Summarize asks for the short version of a conversation.
func Summarize(mail string) (system, prompt string) {
	return voice + " Summaries are for someone deciding what to do with the email.",
		"Summarize this email conversation in at most five short bullet points, newest state first. Start each bullet with '- '. Say what's being asked, who needs to act, and any dates or deadlines. If nothing needs doing, say so in one bullet.\n\n" + clip(mail)
}

// DraftReply turns a few words from the user into a reply.
func DraftReply(note, original, senderName string) (system, prompt string) {
	who := ""
	if senderName != "" {
		who = " The person signing it is " + senderName + "."
	}
	return voice + " You write replies the user then edits and sends." + who,
		"Write a reply to the email below. The reply must say this, in the user's own words expanded into a natural message: \"" + strings.TrimSpace(note) + "\"\n\nKeep it as short as the point allows. Open with a greeting using the sender's first name if known. Don't add a signature or sign-off name; the app adds the signature. Use plain paragraphs separated by blank lines.\n\nThe conversation being replied to, oldest first:\n\n" + clip(original)
}

// DraftNew writes a fresh message from a few words, with nothing to reply to.
func DraftNew(note, senderName string) (system, prompt string) {
	who := ""
	if senderName != "" {
		who = " The person signing it is " + senderName + "."
	}
	return voice + " You write emails the user then edits and sends." + who,
		"Write an email that says this, in the user's own words expanded into a natural message: \"" + strings.TrimSpace(note) + "\"\n\nKeep it as short as the point allows. Don't add a subject line, a signature or a sign-off name; the app adds the signature. Use plain paragraphs separated by blank lines."
}

// Rewrite modes, in the order the menu shows them.
var Modes = []struct{ ID, Label string }{
	{"shorter", "Make it shorter"},
	{"professional", "More professional"},
	{"friendlier", "Friendlier"},
	{"grammar", "Fix spelling and grammar"},
}

var modeAsk = map[string]string{
	"shorter":      "Make this shorter. Keep every point and every fact, cut the padding.",
	"professional": "Make this read more professionally without making it stiff or adding jargon. Keep the meaning and the facts.",
	"friendlier":   "Make this warmer and friendlier while keeping it brief. Keep the meaning and the facts.",
	"grammar":      "Fix spelling, grammar and punctuation only. Don't change the wording, tone or meaning beyond that.",
}

// Rewrite changes the user's own text. ok is false for an unknown mode.
func Rewrite(mode, text string) (system, prompt string, ok bool) {
	ask, ok := modeAsk[mode]
	if !ok {
		return "", "", false
	}
	return voice + " You edit the user's own draft. Keep their voice; don't replace it with a template.",
		fmt.Sprintf("%s Keep the same paragraph breaks where they make sense. Return only the rewritten text.\n\nText:\n\n%s", ask, clip(text)), true
}

// Search turns a plain-English request into the search box's operators.
// today anchors words like "yesterday" and "last month"; labels are the
// user's label names, so "tagged clients" can become label:Clients.
func Search(ask string, today time.Time, labels []string) (system, prompt string) {
	lb := "The user has no labels."
	if len(labels) > 0 {
		lb = "The user's labels are: " + strings.Join(labels, ", ") + ". Use label:<name> only when the request names one of them."
	}
	return "You translate what someone wants to find in their email into a search query. Output only the query on one line, nothing else: no quotes around it, no explanation.",
		fmt.Sprintf(`Today is %s. Search syntax, the same as Gmail's:
- plain words match anywhere (subject, sender, body)
- from:<name or address>   to:<name or address>   subject:<words, quoted if more than one word>
- has:attachment
- after:YYYY-MM-DD   before:YYYY-MM-DD   (before is exclusive; "last month" is the whole previous calendar month; "last week" is the previous Monday through Sunday; "this week" starts last Monday; "yesterday" is after:yesterday before:today; "recent" or "lately" means the last 14 days)
- label:<name>
%s
Keep the words that carry meaning (invoice, printer, refund) as plain words. Don't invent names or dates the user didn't imply. If the request is already a query, return it unchanged.

Request: %s`, today.Format("Monday, January 2, 2006"), lb, strings.TrimSpace(ask))
}
