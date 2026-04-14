package tui

import "strings"

// Brand marks are pre-composed unicode block glyphs to render consistently across
// terminals. Prefer these over “drawn” ASCII art for a crisp look.

func brandMark(width int) string {
	if width >= 34 {
		return strings.TrimRight(brandLarge, "\n")
	}
	return strings.TrimRight(brandSmall, "\n")
}

const brandSmall = `
   ▗▄▄▄▖
 ▗▟█▀▀▜█▙▖
▐█▛  ▗▛██▌
▐█▌ ▗▛▗██▌
▝█▙▄▟█▛▘
  ▐██▌
  ▐██▌
`

const brandLarge = `
     ▗▄▄▄▄▖
   ▗▟█▀▀▀▀█▙▖
  ▐█▛   ▗▛██▌
  ▐█▌  ▗▛▗██▌
  ▐█▌ ▗▛▗███▌
  ▝█▙▄▟█▛▀▘
    ▐██▌
    ▐██▌
    ▐██▌
`

