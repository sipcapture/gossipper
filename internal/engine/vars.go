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
	// scratch is reused by Snapshot() to avoid map allocations on hot path
	scratch map[string]string
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
	capacity := len(v.local) + len(v.globalNames) + len(v.userNames)
	if v.scratch == nil {
		v.scratch = make(map[string]string, capacity)
	}
	clear(v.scratch)
	for k, val := range v.local {
		v.scratch[k] = val
	}
	v.scopes.mu.RLock()
	for name := range v.globalNames {
		v.scratch[name] = v.scopes.global[name]
	}
	if scoped, exists := v.scopes.users[v.userID]; exists {
		for name := range v.userNames {
			v.scratch[name] = scoped[name]
		}
	}
	v.scopes.mu.RUnlock()
	return v.scratch
}
