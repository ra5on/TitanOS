package agent

// Drafts and the context of the panel (1.13). Older versions ignore both
// fields of an entry: the step stays readable, without its card.

// Draft is what a tool made in the mode "Nur Vorschläge" instead of
// writing: the program App (mail, calendar, contacts, tasks) opens it in
// its editor with Fields filled in, and the person saves or sends it
// there. Kind names what it is (mail: new, reply, move; calendar: event;
// contacts: contact; tasks: task). Fields are plain values for the editor
// (texts, lists of texts, numbers, true/false); the editor treats them as
// text the person checks, never as an order.
type Draft struct {
	App    string         `json:"app"`
	Kind   string         `json:"kind"`
	Fields map[string]any `json:"fields"`
}

// ChatContext names what was open in a program when the person wrote in
// its panel. It is a reference: the server writes one line about it into
// the prompt (with names cleaned), the model reads the thing itself with
// its tools and the person's rights.
type ChatContext struct {
	App   string    `json:"app"`
	Mail  *MailRef  `json:"mail,omitempty"`
	Card  *CardRef  `json:"card,omitempty"`
	Date  string    `json:"date,omitempty"`
	Event *EventRef `json:"event,omitempty"`
}

// MailRef names a mail: mailbox (account id of Mail), folder and uid.
type MailRef struct {
	A   string `json:"a"`
	F   string `json:"f"`
	UID uint32 `json:"uid"`
}

// CardRef names a contact: address book and file.
type CardRef struct {
	Book string `json:"book"`
	ID   string `json:"id"`
}

// EventRef names an event: calendar, file and occurrence.
type EventRef struct {
	Cal        string `json:"cal"`
	ID         string `json:"id"`
	Occurrence string `json:"occurrence,omitempty"`
}
