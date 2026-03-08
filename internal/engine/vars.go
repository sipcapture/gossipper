package engine

import "sync"

type scopedVars struct {
	mu     sync.RWMutex
	global map[string]string
	users  map[int]map[string]string
}

func newScopedVars() *scopedVars {
	return &scopedVars{
		global: make(map[string]string),
		users:  make(map[int]map[string]string),
	}
}

type varStore struct {
	local       map[string]string
	globalNames map[string]struct{}
	userNames   map[string]struct{}
	scopes      *scopedVars
	userID      int
}

func newVarStore(scopes *scopedVars, globalNames, userNames []string, userID int) *varStore {
	store := &varStore{
		local:       make(map[string]string),
		globalNames: make(map[string]struct{}, len(globalNames)),
		userNames:   make(map[string]struct{}, len(userNames)),
		scopes:      scopes,
		userID:      userID,
	}
	for _, name := range globalNames {
		store.globalNames[name] = struct{}{}
	}
	for _, name := range userNames {
		store.userNames[name] = struct{}{}
	}
	return store
}

func (v *varStore) Get(name string) string {
	if _, ok := v.globalNames[name]; ok {
		v.scopes.mu.RLock()
		defer v.scopes.mu.RUnlock()
		return v.scopes.global[name]
	}
	if _, ok := v.userNames[name]; ok {
		v.scopes.mu.RLock()
		defer v.scopes.mu.RUnlock()
		if scoped, exists := v.scopes.users[v.userID]; exists {
			return scoped[name]
		}
		return ""
	}
	return v.local[name]
}

func (v *varStore) Set(name, value string) {
	if _, ok := v.globalNames[name]; ok {
		v.scopes.mu.Lock()
		v.scopes.global[name] = value
		v.scopes.mu.Unlock()
		return
	}
	if _, ok := v.userNames[name]; ok {
		v.scopes.mu.Lock()
		if _, exists := v.scopes.users[v.userID]; !exists {
			v.scopes.users[v.userID] = make(map[string]string)
		}
		v.scopes.users[v.userID][name] = value
		v.scopes.mu.Unlock()
		return
	}
	v.local[name] = value
}

func (v *varStore) Snapshot() map[string]string {
	out := make(map[string]string, len(v.local))
	for k, val := range v.local {
		out[k] = val
	}
	v.scopes.mu.RLock()
	defer v.scopes.mu.RUnlock()
	for name := range v.globalNames {
		out[name] = v.scopes.global[name]
	}
	if scoped, exists := v.scopes.users[v.userID]; exists {
		for name := range v.userNames {
			out[name] = scoped[name]
		}
	}
	return out
}
