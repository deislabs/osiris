package activator

import (
	"fmt"
)

func getKey(namespace, name string) string {
	return fmt.Sprintf("%s:%s", namespace, name)
}
