package bot

import (
	"fmt"
	"testing"
)

func TestEmoji(t *testing.T) {
	for _, s := range emojiWin() {
		fmt.Println(s)
	}

	for _, s := range emojiLoose() {
		fmt.Println(s)
	}

}
