// +build tools

/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

/*
This tool prints all the titles of all PRs from previous release to HEAD.
This needs to be run *before* a tag is created.

Use these as the base of your release notes.
*/

const (
	features      = ":sparkles: New Features"
	bugs          = ":bug: Bug Fixes"
	documentation = ":book: Documentation"
	proposals     = ":memo: Proposals"
	warning       = ":warning: Breaking Changes"
	tests         = ":green_heart: Testing"
	other         = ":seedling: Others"
	unknown       = ":question: Sort these by hand"
)

var (
	outputOrder = []string{
		proposals,
		warning,
		features,
		bugs,
		tests,
		other,
		documentation,
		unknown,
	}

	fromTag = flag.String("from", "", "The tag or commit to start from.")
)

func main() {
	flag.Parse()
	os.Exit(run())
}

func lastTag() string {
	if fromTag != nil && *fromTag != "" {
		return *fromTag
	}
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
	out, err := cmd.Output()
	if err != nil {
		return firstCommit()
	}
	return string(bytes.TrimSpace(out))
}

func firstCommit() string {
	cmd := exec.Command("git", "rev-list", "--max-parents=0", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "UNKNOWN"
	}
	return string(bytes.TrimSpace(out))
}

func run() int {
	lastTag := lastTag()
	cmd := exec.Command("git", "rev-list", lastTag+"..HEAD", "--merges", "--pretty=format:%B")

	merges := map[string][]string{
		features:      {},
		bugs:          {},
		tests:         {},
		documentation: {},
		warning:       {},
		other:         {},
		unknown:       {},
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("Error")
		fmt.Println(string(out))
		return 1
	}

	commits := []*commit{}
	outLines := strings.Split(string(out), "\n")
	for _, line := range outLines {
		line = strings.TrimSpace(line)
		last := len(commits) - 1
		switch {
		case strings.HasPrefix(line, "commit"):
			commits = append(commits, &commit{})
		case strings.HasPrefix(line, "Merge"):
			commits[last].merge = line
			continue
		case line == "":
		default:
			commits[last].body = line
		}
	}

	for _, c := range commits {
		body := strings.TrimSpace(c.body)
		var key, prNumber, fork string
		switch {
		case strings.HasPrefix(body, ":hammer:"), strings.HasPrefix(body, "🔨"):
			// do nothing for chores, we exclude them from release notes
			continue
		case strings.HasPrefix(body, ":sparkles:"), strings.HasPrefix(body, "✨"):
			key = features
			body = strings.TrimPrefix(body, ":sparkles:")
			body = strings.TrimPrefix(body, "✨")
		case strings.HasPrefix(body, ":bug:"), strings.HasPrefix(body, "🐛"):
			key = bugs
			body = strings.TrimPrefix(body, ":bug:")
			body = strings.TrimPrefix(body, "🐛")
		case strings.HasPrefix(body, ":book:"), strings.HasPrefix(body, "📖"):
			key = documentation
			body = strings.TrimPrefix(body, ":book:")
			body = strings.TrimPrefix(body, "📖")
			if strings.Contains(body, "CAEP") || strings.Contains(body, "proposal") {
				key = proposals
			}
		case strings.HasPrefix(body, ":seedling:"), strings.HasPrefix(body, "🌱"):
			key = other
			body = strings.TrimPrefix(body, ":seedling:")
			body = strings.TrimPrefix(body, "🌱")
		case strings.HasPrefix(body, ":warning:"), strings.HasPrefix(body, "⚠️"):
			key = warning
			body = strings.TrimPrefix(body, ":warning:")
			body = strings.TrimPrefix(body, "⚠️")
		case strings.HasPrefix(body, ":green_heart:"), strings.HasPrefix(body, "💚"):
			key = tests
			body = strings.TrimPrefix(body, ":green_heart:")
			body = strings.TrimPrefix(body, "💚")
		default:
			key = unknown
		}

		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		body = fmt.Sprintf("- %s", body)
		fmt.Sscanf(c.merge, "Merge pull request %s from %s", &prNumber, &fork)
		if key == documentation {
			merges[key] = append(merges[key], prNumber)
			continue
		}
		merges[key] = append(merges[key], formatMerge(body, prNumber))
	}

	// TODO Turn this into a link (requires knowing the project name + organization)
	fmt.Printf("Changes since %v\n---\n", lastTag)

	for _, key := range outputOrder {
		mergeslice := merges[key]
		if len(mergeslice) == 0 {
			continue
		}

		switch key {
		case documentation:
			fmt.Printf(
				":book: Additionally, there have been %d contributions to our documentation and book. (%s) \n\n",
				len(mergeslice),
				strings.Join(mergeslice, ", "),
			)
		default:
			fmt.Println("## " + key)
			for _, merge := range mergeslice {
				fmt.Println(merge)
			}
			fmt.Println()
		}

	}

	fmt.Println("")
	fmt.Println("_Thanks to all our contributors!_ 😊")

	return 0
}

type commit struct {
	merge string
	body  string
}

func formatMerge(line, prNumber string) string {
	if prNumber == "" {
		return line
	}
	return fmt.Sprintf("%s (%s)", line, prNumber)
}
