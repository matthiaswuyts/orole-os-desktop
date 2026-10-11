package chat

import (
	"context"
	"iter"
	"slices"
	"strconv"
	"strings"
)

// The history queries load only what the model is shown: the text-bearing parts of each message
// (text, question, report, attachment names), not tool rows, citations, reasoning or UI specs, and
// never the usage/error/model columns. SQLite extracts them with json_each, so the dropped parts are
// never copied into Go. A projection is a valid parts array: historyText and attribute work on it
// unchanged.
const (
	summaryPage = 200  // messages per query when streaming the transcript to the summariser
	maxTailRows = 5000 // hard cap on messages loaded for one request

	fullText = `json_extract(p.value, '$.text')`
	// clipText keeps a bit more than summaryMessageBytes characters: enough for the summariser to cut
	// at its byte limit exactly as it would on the full text (a character is at least one byte).
	clipText = `substr(json_extract(p.value, '$.text'), 1, 2001)`

	projectionSQL = `SELECT seq, role, author, status, COALESCE((SELECT json_group_array(json_object(
			'type', json_extract(p.value, '$.type'),
			'text', {text},
			'name', json_extract(p.value, '$.name'),
			'mime', json_extract(p.value, '$.mime')) ORDER BY p.key)
		FROM json_each(messages.parts) p
		WHERE json_extract(p.value, '$.type') IN ('text', 'question', 'report', 'attachment')), '[]') FROM messages`
)

func projection(textExpr string) string {
	return strings.Replace(projectionSQL, "{text}", textExpr, 1)
}

func scanHistory(r scanner) (Message, error) {
	var m Message
	var parts string
	if err := r.Scan(&m.Seq, &m.Role, &m.Author, &m.Status, &parts); err != nil {
		return Message{}, err
	}
	m.Parts = RawParts(parts)
	return m, nil
}

// historyTail is the newest part of the unsummarised conversation in ascending order, with the
// estimated token cost of every message computed once.
type historyTail struct {
	msgs   []Message
	tokens []int64
	total  int64
	// older reports that unsummarised messages exist before msgs[0].
	older bool
}

// tail loads the newest messages with afterSeq < seq < beforeSeq, newest first, until their summed
// estimated tokens reach budgetTokens (<= 0: no token limit) or maxRows messages are loaded. The
// message that crosses the budget is included, so a tail that stopped early holds at least the budget.
func (s *store) tail(ctx context.Context, botID string, afterSeq, beforeSeq, budgetTokens int64, maxRows int) (historyTail, error) {
	maxRows = min(maxRows, maxTailRows)
	rows, err := s.db.QueryContext(ctx, projection(fullText)+` WHERE bot_id=? AND seq>? AND seq<? ORDER BY seq DESC LIMIT ?`,
		botID, afterSeq, beforeSeq, maxRows+1)
	if err != nil {
		return historyTail{}, err
	}
	defer rows.Close()
	var t historyTail
	for rows.Next() {
		if len(t.msgs) == maxRows || (budgetTokens > 0 && t.total >= budgetTokens) {
			t.older = true
			break
		}
		m, err := scanHistory(rows)
		if err != nil {
			return historyTail{}, err
		}
		tok := messageTokens(m)
		t.msgs = append(t.msgs, m)
		t.tokens = append(t.tokens, tok)
		t.total += tok
	}
	if err := rows.Err(); err != nil {
		return historyTail{}, err
	}
	slices.Reverse(t.msgs)
	slices.Reverse(t.tokens)
	return t, nil
}

// countBetween counts the messages with afterSeq < seq < beforeSeq.
func (s *store) countBetween(ctx context.Context, botID string, afterSeq, beforeSeq int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE bot_id=? AND seq>? AND seq<?`, botID, afterSeq, beforeSeq).Scan(&n)
	return n, err
}

// transcript streams the messages with afterSeq < seq <= uptoSeq in ascending order for the
// summariser, a page at a time, with each text part clipped to what the summariser will read.
func (s *store) transcript(ctx context.Context, botID string, afterSeq, uptoSeq int64) iter.Seq2[Message, error] {
	return func(yield func(Message, error) bool) {
		query := projection(clipText) + ` WHERE bot_id=? AND seq>? AND seq<=? ORDER BY seq LIMIT ` + strconv.Itoa(summaryPage)
		for {
			msgs, err := s.history(ctx, query, botID, afterSeq, uptoSeq)
			if err != nil {
				yield(Message{}, err)
				return
			}
			for _, m := range msgs {
				if !yield(m, nil) {
					return
				}
			}
			if len(msgs) < summaryPage {
				return
			}
			afterSeq = msgs[len(msgs)-1].Seq
		}
	}
}

func (s *store) history(ctx context.Context, query string, args ...any) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanHistory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
