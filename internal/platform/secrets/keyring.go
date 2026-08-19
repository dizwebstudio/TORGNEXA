package secrets

import (
	"context"
	"errors"
)

// StaticKeyring is a small local key source intended for Community deployments.
// Callers must construct it from an external secret source (environment/file/secret mount), never from PostgreSQL.
type StaticKeyring struct {
	current string
	keys    map[string]MasterKey
}

func NewStaticKeyring(current string, material map[string][]byte) (*StaticKeyring, error) {
	if current == "" || len(material) == 0 {
		return nil, ErrKeyUnavailable
	}
	keys := make(map[string]MasterKey, len(material))
	for id, raw := range material {
		key, err := NewMasterKey(id, raw)
		if err != nil {
			return nil, ErrKeyUnavailable
		}
		keys[id] = key
	}
	if _, ok := keys[current]; !ok {
		return nil, ErrKeyUnavailable
	}
	return &StaticKeyring{current: current, keys: keys}, nil
}

func (keyring *StaticKeyring) Current(ctx context.Context) (MasterKey, error) {
	if ctx == nil {
		return MasterKey{}, errors.New("secrets keyring: context is required")
	}
	if err := ctx.Err(); err != nil {
		return MasterKey{}, err
	}
	if keyring == nil {
		return MasterKey{}, ErrKeyUnavailable
	}
	key, ok := keyring.keys[keyring.current]
	if !ok {
		return MasterKey{}, ErrKeyUnavailable
	}
	return key, nil
}

func (keyring *StaticKeyring) ByID(ctx context.Context, id string) (MasterKey, error) {
	if ctx == nil {
		return MasterKey{}, errors.New("secrets keyring: context is required")
	}
	if err := ctx.Err(); err != nil {
		return MasterKey{}, err
	}
	if keyring == nil || !validKeyID(id) {
		return MasterKey{}, ErrKeyUnavailable
	}
	key, ok := keyring.keys[id]
	if !ok {
		return MasterKey{}, ErrKeyUnavailable
	}
	return key, nil
}
