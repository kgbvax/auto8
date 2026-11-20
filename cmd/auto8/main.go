// SPDX-License-Identifier: AGPL-3.0-or-later

// Command auto8 automates chasing in FT8 and FT4: it reads the decodes of
// WSJT-X and MSHV and tells them which needed station to call. See CLAUDE.md.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	fs := flag.NewFlagSet("auto8", flag.ExitOnError)
	_ = fs.Parse(os.Args[1:])
	fmt.Println("auto8: nothing to do yet")
}
