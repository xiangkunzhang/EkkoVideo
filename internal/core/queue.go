package core

// Queue manages an ordered playback list (episodes, folder files, or channels).
type Queue struct {
	Items   []Item
	Current int
}

func NewQueue(items []Item, current int) *Queue {
	if len(items) == 0 {
		return &Queue{Current: -1}
	}
	if current < 0 || current >= len(items) {
		current = 0
	}
	return &Queue{Items: append([]Item(nil), items...), Current: current}
}

func (q *Queue) Active() (Item, bool) {
	if q == nil || q.Current < 0 || q.Current >= len(q.Items) {
		return Item{}, false
	}
	return q.Items[q.Current], true
}

func (q *Queue) HasNext() bool {
	return q != nil && q.Current >= 0 && q.Current+1 < len(q.Items)
}

func (q *Queue) HasPrev() bool {
	return q != nil && q.Current > 0 && len(q.Items) > 0
}

func (q *Queue) Next() (Item, bool) {
	if !q.HasNext() {
		return Item{}, false
	}
	q.Current++
	return q.Items[q.Current], true
}

func (q *Queue) Prev() (Item, bool) {
	if !q.HasPrev() {
		return Item{}, false
	}
	q.Current--
	return q.Items[q.Current], true
}

func (q *Queue) Jump(index int) (Item, bool) {
	if q == nil || index < 0 || index >= len(q.Items) {
		return Item{}, false
	}
	q.Current = index
	return q.Items[q.Current], true
}
