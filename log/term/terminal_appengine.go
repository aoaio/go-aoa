// +build appengine

package term

func IsTty(fd uintptr) bool {
	return false
}
