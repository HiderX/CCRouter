package session

import (
	"fmt"

	"ccrouter/internal/store"
	"ccrouter/internal/usage"
)

// handOff rebinds conversation v to another account. Both profiles share the conversation
// directories, so the one local copy stays where it is; the new account starts with a
// cold prompt cache.
func handOff(s *store.Store, from, to store.Account, v store.Session) error {
	for _, a := range []store.Account{from, to} {
		if e := usage.Share(a.Profile); e != nil || !usage.Shared(a.Profile) {
			return fmt.Errorf("「%s」的对话目录尚未共享，无法换号：%v", a.Name, e)
		}
	}
	// Count the conversation so far under the account that ran it, before the binding moves.
	if src := usage.Transcript(from.Profile, v.CWD, v.ID); src != "" {
		if e := (&usage.Indexer{Store: s}).File(from, src); e != nil {
			return e
		}
	}
	return s.Follow(store.Session{ID: v.ID, AccountID: to.ID, CWD: v.CWD})
}
