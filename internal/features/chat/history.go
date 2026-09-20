package chat

const DefaultHistoryLimit = 200

// History owns submitted prompt history and its navigation cursor. It has no
// terminal or UI dependencies, so command-line frontends can share it.
type History struct {
	entries []string
	index   int
	draft   string
	limit   int
}

func NewHistory(entries []string, limit int) History {
	h := History{limit: limit}
	h.SetEntries(entries)
	return h
}

func (h *History) SetEntries(entries []string) {
	h.entries = append(h.entries[:0], entries...)
	if h.limit <= 0 {
		h.limit = DefaultHistoryLimit
	}
	if len(h.entries) > h.limit {
		h.entries = append([]string(nil), h.entries[len(h.entries)-h.limit:]...)
	}
	h.index = len(h.entries)
	h.draft = ""
}

func (h History) Entries() []string { return append([]string(nil), h.entries...) }
func (h History) Len() int          { return len(h.entries) }
func (h History) Index() int        { return h.index }

func (h History) Last() (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}
	return h.entries[len(h.entries)-1], true
}

func (h *History) Add(entry string) {
	h.entries = append(h.entries, entry)
	if h.limit <= 0 {
		h.limit = DefaultHistoryLimit
	}
	if len(h.entries) > h.limit {
		h.entries = append([]string(nil), h.entries[len(h.entries)-h.limit:]...)
	}
	h.index = len(h.entries)
	h.draft = ""
}

func (h *History) Up(draft string) (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}
	if h.index == len(h.entries) {
		h.draft = draft
	}
	if h.index == 0 {
		return "", false
	}
	h.index--
	return h.entries[h.index], true
}

func (h *History) Down() (string, bool) {
	if h.index < len(h.entries)-1 {
		h.index++
		return h.entries[h.index], true
	}
	if h.index == len(h.entries)-1 {
		h.index = len(h.entries)
		return h.draft, true
	}
	return "", false
}
