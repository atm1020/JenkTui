package folder

import (
	"github.com/atm1020/jenktui/src/store"

	"github.com/atm1020/gojenkins"
)

// FavoritesLoadedMsg is emitted once, in response to Init loading the
// current favorites so rows can be marked.
type FavoritesLoadedMsg struct {
	Favorites []store.FavoriteRecord
	Error     error
}

// ChildrenLoadedMsg carries the result of a container's one-shot child
// request.
//
// ContainerURL and Generation scope the result to the request that asked for
// it. Jenkins can answer long after the user has moved on, and the router
// keeps a left page alive in its history — without this scoping a late reply
// would repopulate a page the user is no longer looking at, or overwrite the
// newer list a re-entered page had already loaded.
type ChildrenLoadedMsg struct {
	ContainerURL string
	Generation   int
	Children     []gojenkins.DetailedInnerJob
	Error        error
}

// FavoriteToggledMsg is emitted after a toggle's store write completes.
type FavoriteToggledMsg struct {
	URL       string
	Favorited bool
}
