package jdb

import (
	"fmt"

	"github.com/dgraph-io/badger/v3"
	"github.com/sirupsen/logrus"
)

type commandHandlerFn func(*Hub, Client, Request)

var handlers = map[string]commandHandlerFn{
	CmdReadKey:           cmdReadKey,
	CmdReadBulk:          cmdReadBulk,
	CmdReadPrefix:        cmdReadPrefix,
	CmdWriteKey:          cmdWriteKey,
	CmdWriteBulk:         cmdWriteBulk,
	CmdSubscribeKey:      cmdSubscribeKey,
	CmdUnsubscribeKey:    cmdUnsubscribeKey,
	CmdSubscribePrefix:   cmdSubscribePrefix,
	CmdUnsubscribePrefix: cmdUnsubscribePrefix,
	CmdProtoVersion:      cmdProtoVersion,
	CmdListKeys:          cmdListKeys,
}

func cmdReadKey(h *Hub, client Client, msg Request) {
	// Check params
	key, ok := msg.Data["key"].(string)
	if !ok {
		sendErr(client, ErrMissingParam, "invalid or missing 'key' parameter", msg.RequestID)
		return
	}

	// Remap key if necessary
	options := client.Options()
	realKey := options.Namespace + key

	err := h.db.View(func(tx *badger.Txn) error {
		val, err := tx.Get([]byte(realKey))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				client.SendJSON(Response{"response", true, msg.RequestID, ""})
				h.logger.WithFields(logrus.Fields{
					"client": client.UID(),
					"key":    realKey,
				}).Debug("get for inexistant key")
				return nil
			}
			return err
		}
		byt, err := val.ValueCopy(nil)
		if err != nil {
			return err
		}
		client.SendJSON(Response{"response", true, msg.RequestID, string(byt)})
		h.logger.WithFields(logrus.Fields{
			"client": client.UID(),
			"key":    realKey,
		}).Debug("get key")
		return nil
	})

	if err != nil {
		sendErr(client, ErrServerError, err.Error(), msg.RequestID)
		return
	}
}

func cmdReadBulk(h *Hub, client Client, msg Request) {
	// Check params
	keys, ok := msg.Data["keys"].([]interface{})
	if !ok {
		sendErr(client, ErrMissingParam, "invalid or missing 'keys' parameter", msg.RequestID)
		return
	}

	options := client.Options()
	realKeys := make([]string, len(keys))
	for index, key := range keys {
		// Remap key if necessary
		realKeys[index], ok = key.(string)
		if !ok {
			sendErr(client, ErrMissingParam, "invalid entry in 'keys' parameter", msg.RequestID)
			return
		}
		realKeys[index] = options.Namespace + realKeys[index]
	}

	out := make(map[string]string)
	err := h.db.View(func(tx *badger.Txn) error {
		for index, key := range realKeys {
			val, err := tx.Get([]byte(key))
			if err != nil {
				if err == badger.ErrKeyNotFound {
					out[key] = ""
					continue
				}
				return err
			}
			byt, err := val.ValueCopy(nil)
			if err != nil {
				return err
			}
			out[keys[index].(string)] = string(byt)
		}
		return nil
	})
	if err != nil {
		sendErr(client, ErrServerError, "server error: "+err.Error(), msg.RequestID)
		return
	}
	h.logger.WithFields(logrus.Fields{
		"client": client.UID(),
		"keys":   realKeys,
	}).Debug("get multi key")
	client.SendJSON(Response{"response", true, msg.RequestID, out})
}

func cmdReadPrefix(h *Hub, client Client, msg Request) {
	// Check params
	prefix, ok := msg.Data["prefix"].(string)
	if !ok {
		sendErr(client, ErrMissingParam, "invalid or missing 'prefix' parameter", msg.RequestID)
		return
	}

	// Remap key if necessary
	options := client.Options()
	realPrefix := options.Namespace + prefix

	out := make(map[string]string)
	err := h.db.View(func(tx *badger.Txn) error {
		opt := badger.DefaultIteratorOptions
		opt.Prefix = []byte(realPrefix)
		it := tx.NewIterator(opt)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			byt, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			key := string(item.Key())
			out[prefix+key[len(realPrefix):]] = string(byt)
		}
		return nil
	})
	if err != nil {
		sendErr(client, ErrServerError, err.Error(), msg.RequestID)
		return
	}
	h.logger.WithFields(logrus.Fields{
		"client": client.UID(),
		"prefix": prefix,
	}).Debug("get all (prefix)")
	client.SendJSON(Response{"response", true, msg.RequestID, out})
}

func cmdWriteKey(h *Hub, client Client, msg Request) {
	// Check params
	key, ok := msg.Data["key"].(string)
	if !ok {
		sendErr(client, ErrMissingParam, "invalid or missing 'key' parameter", msg.RequestID)
		return
	}
	data, ok := msg.Data["data"].(string)
	if !ok {
		sendErr(client, ErrMissingParam, "invalid or missing 'data' parameter", msg.RequestID)
		return
	}

	// Remap key if necessary
	options := client.Options()
	realKey := options.Namespace + key

	err := h.db.Update(func(tx *badger.Txn) error {
		return tx.Set([]byte(realKey), []byte(data))
	})
	if err != nil {
		sendErr(client, ErrServerError, err.Error(), msg.RequestID)
		return
	}
	// Send OK response
	client.SendJSON(Response{"response", true, msg.RequestID, nil})

	h.logger.WithFields(logrus.Fields{
		"client": client.UID(),
		"key":    realKey,
	}).Debug("modified key")
}

func cmdWriteBulk(h *Hub, client Client, msg Request) {
	options := client.Options()
	// Copy data over
	kvs := make(map[string]string)
	for k, v := range msg.Data {
		strval, ok := v.(string)
		if !ok {
			sendErr(client, ErrInvalidFmt, fmt.Sprintf("invalid value for key \"%s\"", k), msg.RequestID)
			return
		}
		kvs[options.Namespace+k] = strval
	}

	err := h.db.Update(func(tx *badger.Txn) error {
		for k, v := range kvs {
			err := tx.Set([]byte(k), []byte(v))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		sendErr(client, ErrServerError, err.Error(), msg.RequestID)
		return
	}
	// Send OK response
	client.SendJSON(Response{"response", true, msg.RequestID, nil})

	h.logger.WithFields(logrus.Fields{
		"client": client.UID(),
	}).Debug("bulk modify keys")
}

func cmdSubscribeKey(h *Hub, client Client, msg Request) {
	// Check params
	key, ok := msg.Data["key"].(string)
	if !ok {
		sendErr(client, ErrMissingParam, "invalid or missing 'key' parameter", msg.RequestID)
		return
	}

	// Remap key if necessary
	options := client.Options()
	realKey := options.Namespace + key

	if err := dbSubscribeToKey(h.memdb, client, realKey); err != nil {
		sendErr(client, ErrServerError, err.Error(), msg.RequestID)
		return
	}
	h.logger.WithFields(logrus.Fields{
		"client": client.UID(),
		"key":    realKey,
	}).Debug("subscribed to key")
	// Send OK response
	client.SendJSON(Response{"response", true, msg.RequestID, nil})
}

func cmdSubscribePrefix(h *Hub, client Client, msg Request) {
	// Check params
	prefix, ok := msg.Data["prefix"].(string)
	if !ok {
		sendErr(client, ErrMissingParam, "invalid or missing 'prefix' parameter", msg.RequestID)
		return
	}

	// Remap key if necessary
	options := client.Options()
	realPrefix := options.Namespace + prefix

	if err := dbSubscribeToPrefix(h.memdb, client, realPrefix); err != nil {
		sendErr(client, ErrServerError, err.Error(), msg.RequestID)
		return
	}
	h.logger.WithFields(logrus.Fields{
		"client": client.UID(),
		"prefix": realPrefix,
	}).Debug("subscribed to prefix")
	// Send OK response
	client.SendJSON(Response{"response", true, msg.RequestID, nil})
}

func cmdUnsubscribeKey(h *Hub, client Client, msg Request) {
	// Check params
	key, ok := msg.Data["key"].(string)
	if !ok {
		sendErr(client, ErrMissingParam, "invalid or missing 'key' parameter", msg.RequestID)
		return
	}

	// Remap key if necessary
	options := client.Options()
	realKey := options.Namespace + key

	if err := dbUnsubscribeFromKey(h.memdb, client, realKey); err != nil {
		sendErr(client, ErrServerError, err.Error(), msg.RequestID)
		return
	}
	h.logger.WithFields(logrus.Fields{
		"client": client.UID(),
		"key":    realKey,
	}).Debug("unsubscribed to key")
	// Send OK response
	client.SendJSON(Response{"response", true, msg.RequestID, nil})

}

func cmdUnsubscribePrefix(h *Hub, client Client, msg Request) {
	// Check params
	prefix, ok := msg.Data["prefix"].(string)
	if !ok {
		sendErr(client, ErrMissingParam, "invalid or missing 'prefix' parameter", msg.RequestID)
		return
	}

	// Remap key if necessary
	options := client.Options()
	realPrefix := options.Namespace + prefix

	// Add to prefix subscriber map
	if err := dbUnsubscribeFromPrefix(h.memdb, client, realPrefix); err != nil {
		sendErr(client, ErrServerError, err.Error(), msg.RequestID)
		return
	}
	h.logger.WithFields(logrus.Fields{
		"client": client.UID(),
		"prefix": realPrefix,
	}).Debug("unsubscribed from prefix")

	// Send OK response
	client.SendJSON(Response{"response", true, msg.RequestID, nil})
}

func cmdProtoVersion(_ *Hub, client Client, msg Request) {
	client.SendJSON(Response{"response", true, msg.RequestID, ProtoVersion})
}

func cmdListKeys(h *Hub, client Client, msg Request) {
	var prefix string

	// Check params
	if prefixRaw, ok := msg.Data["prefix"]; ok {
		prefix, _ = prefixRaw.(string)
	}

	// Remap key if necessary
	options := client.Options()
	realPrefix := options.Namespace + prefix

	var out []string
	err := h.db.View(func(tx *badger.Txn) error {
		opt := badger.DefaultIteratorOptions
		opt.Prefix = []byte(realPrefix)
		it := tx.NewIterator(opt)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			out = append(out, string(item.Key()))
		}
		return nil
	})
	if err != nil {
		sendErr(client, ErrServerError, err.Error(), msg.RequestID)
		return
	}
	// If no keys are found, return empty array instead of null
	if len(out) < 1 {
		out = []string{}
	}
	h.logger.WithFields(logrus.Fields{
		"client": client.UID(),
		"prefix": prefix,
	}).Debug("list keys")
	client.SendJSON(Response{"response", true, msg.RequestID, out})
}
