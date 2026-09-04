package configsync

import "fmt"

type Dropped struct {
	Path  string
	Value any
}

func Merge(schema, user, opinionated Tree) (Tree, []Dropped) {
	var dropped []Dropped

	out := mergeTree(schema, user, opinionated, "", &dropped)

	return out, dropped
}

func mergeTree(schema, user, opinionated Tree, prefix string, dropped *[]Dropped) Tree {
	out := make(Tree, len(schema))

	for key, node := range schema {
		path := join(prefix, key)
		userVal, hasUser := user[key]
		opVal, hasOp := opinionated[key]

		if key == "boards" && prefix == "" {
			if list, ok := userVal.([]any); ok {
				out[key] = mergeBoardsList(node, list, dropped)

				continue
			}
		}

		switch n := node.(type) {
		case map[string]any:
			if len(n) == 0 {
				out[key] = pick(hasUser, userVal, Tree{})

				continue
			}

			childUser, _ := userVal.(map[string]any)
			childOp, _ := opVal.(map[string]any)

			if childUser == nil {
				childUser = Tree{}
			}

			if childOp == nil {
				childOp = Tree{}
			}

			if hasUser && userVal != nil {
				if _, isMap := userVal.(map[string]any); !isMap {
					// A scalar where the schema has a mapping cannot be merged;
					// keep the user's value and let validation report it.
					out[key] = userVal

					continue
				}
			}

			out[key] = mergeTree(n, childUser, childOp, path, dropped)
		case []any:
			if len(n) == 0 {
				out[key] = pick(hasUser, userVal, []any{})

				continue
			}

			out[key] = scalar(hasUser, userVal, hasOp, opVal, node)
		default:
			out[key] = scalar(hasUser, userVal, hasOp, opVal, node)
		}
	}

	for key, val := range user {
		if _, inSchema := schema[key]; !inSchema {
			*dropped = append(*dropped, Dropped{Path: join(prefix, key), Value: val})
		}
	}

	return out
}

func mergeBoardsList(schemaNode any, list []any, dropped *[]Dropped) []any {
	entrySchema, _ := schemaNode.(map[string]any)
	out := make([]any, len(list))

	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			out[i] = item

			continue
		}

		out[i] = mergeTree(entrySchema, entry, Tree{}, fmt.Sprintf("boards[%d]", i), dropped)
	}

	return out
}

func scalar(hasUser bool, userVal any, hasOp bool, opVal, schemaVal any) any {
	if hasUser {
		return userVal
	}

	if hasOp {
		return opVal
	}

	return schemaVal
}

func pick(has bool, val, fallback any) any {
	if has && val != nil {
		return val
	}

	return fallback
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}

	return prefix + "." + key
}
