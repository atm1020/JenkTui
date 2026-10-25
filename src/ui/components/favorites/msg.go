package favorites

import "github.com/atm1020/jenktui/src/store"

// FavoritesLoadedMsg is emitted once, in response to Init's initial load.
type FavoritesLoadedMsg struct {
	Favorites []store.FavoriteRecord
	Error     error
}

// FavoriteDeletedMsg is emitted after a delete's store write completes.
type FavoriteDeletedMsg struct {
	URL string
}
