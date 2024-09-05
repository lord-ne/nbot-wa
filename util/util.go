package util

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"
)

var (
	rSpaces = regexp.MustCompile(`\s+`)
)

func PanicIfError[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

func NormalizeString(s string) string {
	return rSpaces.ReplaceAllLiteralString(strings.TrimSpace(strings.ToLower(s)), " ")
}

// Generates a random number in [bottom, top)
func RandBetween(bottom int, top int) int {
	return bottom + rand.IntN(top-bottom)
}

func AsMilliseconds(millis int) time.Duration {
	return time.Duration(millis) * time.Millisecond
}

func Remove[T any](slice *[]T, index int) {
	currLen := len(*slice)
	if index >= currLen || index < 0 {
		fmt.Printf("Warning: Trying to remove element %v from slice with %v elements\n", index, currLen)
		return
	}

	// Move the last element to the spot to be removed, and shorten the slice by 1
	if index != currLen {
		(*slice)[index] = (*slice)[currLen]
	}

	(*slice) = (*slice)[:currLen-1]
}

func Filter[T any](slice []T, shouldKeep func(T) bool) []T {
	ret := []T{}

	for _, el := range slice {
		if shouldKeep(el) {
			ret = append(ret, el)
		}
	}

	return ret
}

func New[T any](value T) *T {
	return &value
}

// n between 0 and 99
func ordinalSuffixHelper(n uint8) string {
	if n >= 11 && n <= 13 {
		return "th"
	}

	switch n % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	default:
		return "th"
	}
}

func OrdinalSuffix(n int) string {
	if n < 0 {
		return ordinalSuffixHelper(uint8(-(n % 100)))
	} else {
		return ordinalSuffixHelper(uint8(n % 100))
	}
}

func RemoveAndCheckMatch(re *regexp.Regexp, str string) (bool, string) {
	found := false

	newStr := re.ReplaceAllStringFunc(str, func(s string) string {
		found = true
		return ""
	})

	return found, newStr
}
