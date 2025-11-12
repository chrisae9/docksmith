package main

import (
	"fmt"
	"github.com/chis/docksmith/internal/version"
)

func testParsing() {
	parser := version.NewParser()
	comp := version.NewComparator()

	// Test overseerr versions
	current := "v1.34.0-ls151"
	latest := "v1.22.0-ls3"

	fmt.Printf("Testing: current=%s, latest=%s\n\n", current, latest)

	currentVer := parser.ParseTag(current)
	latestVer := parser.ParseTag(latest)

	if currentVer != nil {
		fmt.Printf("Current parsed: %s (Major=%d, Minor=%d, Patch=%d)\n", 
			currentVer.String(), currentVer.Major, currentVer.Minor, currentVer.Patch)
	} else {
		fmt.Println("Current failed to parse")
	}

	if latestVer != nil {
		fmt.Printf("Latest parsed: %s (Major=%d, Minor=%d, Patch=%d)\n", 
			latestVer.String(), latestVer.Major, latestVer.Minor, latestVer.Patch)
	} else {
		fmt.Println("Latest failed to parse")
	}

	if currentVer != nil && latestVer != nil {
		isNewer := comp.IsNewer(latestVer, currentVer)
		compareResult := comp.Compare(currentVer, latestVer)
		fmt.Printf("\nIsNewer(latest, current): %v\n", isNewer)
		fmt.Printf("Compare(current, latest): %d (1=current>latest, -1=current<latest, 0=equal)\n", compareResult)
		
		if isNewer {
			fmt.Println("Result: UPDATE AVAILABLE")
		} else {
			fmt.Println("Result: UP TO DATE")
		}
	}
}
