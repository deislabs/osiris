package activator

import (
	"fmt"
)

func getKey(namespace string, kind appKind, name string) string {
	return fmt.Sprintf("%s:%s/%s", kind, namespace, name)
}
