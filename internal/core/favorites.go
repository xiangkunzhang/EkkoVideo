package core

import "time"

// FavoriteItem represents a saved series/movie in favorites.json.
type FavoriteItem struct {
	Item    Item      `json:"item"`
	SavedAt time.Time `json:"savedAt"`
}

// Favorites manages user-starred items (main-thread owned).
type Favorites struct {
	Items []FavoriteItem
	byKey map[ItemRef]int
}

func NewFavorites(initial []FavoriteItem) *Favorites {
	f := &Favorites{
		Items: append([]FavoriteItem(nil), initial...),
		byKey: make(map[ItemRef]int, len(initial)),
	}
	f.reindex()
	return f
}

func (f *Favorites) reindex() {
	clear(f.byKey)
	for i, it := range f.Items {
		f.byKey[it.Item.Ref] = i
	}
}

func (f *Favorites) Has(ref ItemRef) bool {
	_, ok := f.byKey[ref]
	return ok
}

func (f *Favorites) Toggle(item Item) bool {
	if f.Has(item.Ref) {
		idx := f.byKey[item.Ref]
		f.Items = append(f.Items[:idx], f.Items[idx+1:]...)
		f.reindex()
		return false
	}
	f.Items = append([]FavoriteItem{{Item: item, SavedAt: time.Now()}}, f.Items...)
	f.reindex()
	return true
}
