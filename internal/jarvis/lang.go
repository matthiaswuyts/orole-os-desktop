package jarvis

import (
	"fmt"
	"strings"
)

// phrases are the daemon's fixed spoken lines and model instructions for one call language. Auto uses the Dutch
// lines: a call starts in Dutch and the bot follows the owner from there. Every opener template has one %s for the
// bot's name and nothing else: an opener never says why the bot is calling (see FallbackOpener).
type phrases struct {
	openers []string // name-only greetings for an outbound call
	inbound string   // greeting when the owner phoned, %s = bot name
	verify  string   // %[1]s = the spaced digits, said twice
	cannot  string   // a turn that cannot run (bot missing, transient error)
	refused string   // prefix of the gate refusal; the gate's reason follows
	failed  string   // prefix of a model or tool error; its first sentence follows
	fillers map[string]string
	filler  string   // default filler
	acks    []string // said when a turn is still silent after a moment ("one moment, I'm on it")
	still   []string // said now and then during a long silence while a tool runs
	bye     string   // goodbye when end_call ended the call without the bot saying one
	writeIn string   // appended to the opener and inbound greeting instructions
}

var english = phrases{
	openers: []string{
		"Hey, it's %s. Got a minute?",
		"Hi, %s here. Do you have a moment?",
		"%s here. Got a minute?",
		"Hey, %s calling. Is now a good time?",
		"It's %s. Have you got a second?",
		"Hey, %s here. Quick one, got a minute?",
	},
	inbound: "Hey, %s here. What do you need?",
	verify:  "Your verification code is %[1]s. Again, %[1]s.",
	cannot:  "I can't do that right now.",
	refused: "I can't act on that: ",
	failed:  " That failed: ",
	fillers: map[string]string{
		"list_tasks": "Checking your tasks.", "task_status": "Looking at that task.", "start_task": "Starting that coding task now.",
		"message_task": "Passing that on.", "stop_task": "Stopping it.", "list_automations": "Checking your automations.",
		"automation_runs": "Checking the runs.", "research": "Starting the research.", "web_search": "Looking that up.",
		"web_fetch": "Reading that.", "switch_bot": "One moment.",
	},
	filler:  "One moment.",
	acks:    []string{"One moment, I'm on it.", "Let me check.", "Sure, one sec."},
	still:   []string{"Still on it.", "Almost there, bear with me.", "Still working on it."},
	bye:     "Okay, talk later!",
	writeIn: " Write it in English.",
}

var dutch = phrases{
	openers: []string{
		"Hoi, met %s. Heb je even?",
		"Hallo, %s hier. Heb je een momentje?",
		"%s hier. Heb je even tijd?",
		"Hoi, je spreekt met %s. Komt het nu uit?",
		"Met %s. Heb je een minuutje?",
		"Hé, %s hier. Even kort, heb je een minuut?",
	},
	inbound: "Hoi, met %s. Wat kan ik voor je doen?",
	verify:  "Je verificatiecode is %[1]s. Nog een keer: %[1]s.",
	cannot:  "Dat lukt me nu even niet.",
	refused: "Dat kan ik niet doen: ",
	failed:  " Dat is mislukt: ",
	fillers: map[string]string{
		"list_tasks": "Ik kijk even naar je taken.", "task_status": "Ik kijk even naar die taak.", "start_task": "Ik start die codetaak nu.",
		"message_task": "Ik geef het door.", "stop_task": "Ik stop hem.", "list_automations": "Ik kijk even naar je automatiseringen.",
		"automation_runs": "Ik bekijk de runs.", "research": "Ik start het onderzoek.", "web_search": "Ik zoek het even op.",
		"web_fetch": "Ik lees het even.", "switch_bot": "Momentje.",
	},
	filler:  "Momentje.",
	acks:    []string{"Momentje, ik ben ermee bezig.", "Even kijken.", "Oké, momentje."},
	still:   []string{"Nog even geduld.", "Ik ben er nog mee bezig.", "Bijna klaar."},
	bye:     "Oké, tot later!",
	writeIn: " Write it in Dutch (Nederlands), as a native speaker would say it on the phone.",
}

// phrasesFor returns the lines of a call language (see PhoneSettings.CallLanguage); anything but English is Dutch.
func phrasesFor(lang string) phrases {
	if lang == LangEnglish {
		return english
	}
	return dutch
}

// fillerLine is the scripted line spoken as a tool starts.
func (p phrases) fillerLine(tool string) string {
	if f, ok := p.fillers[tool]; ok {
		return f
	}
	return p.filler
}

// ackLine picks the acknowledgement for a turn that is taking a moment.
func (p phrases) ackLine(seed int) string { return p.acks[seed%len(p.acks)] }

// stillLine is the nth "still working" line (n from 1).
func (p phrases) stillLine(n int) string { return p.still[(n-1)%len(p.still)] }

// verifyLine reads a verification code twice, digit by digit.
func (p phrases) verifyLine(digits string) string { return fmt.Sprintf(p.verify, spaced(digits)) }

// inboundLine is the fallback greeting when the owner phoned.
func (p phrases) inboundLine(name string) string {
	return fmt.Sprintf(p.inbound, runesCut(normalize(name), maxName))
}

// inboundInstruction asks the model for the greeting of a call the owner placed.
func inboundInstruction(lang, name string) string {
	return fmt.Sprintf("The owner just phoned you. Write your greeting: say your name (%s) naturally and ask what they need. "+
		"One short sentence, never name the app.", name) + phrasesFor(lang).writeIn
}

// fallbackOpener picks a name-only greeting in the call language; see FallbackOpener.
func fallbackOpener(lang, name string, seed int) string {
	list := phrasesFor(lang).openers
	name = runesCut(normalize(name), maxName)
	f := list[((seed%len(list))+len(list))%len(list)]
	return strings.TrimSpace(fmt.Sprintf(f, name))
}
