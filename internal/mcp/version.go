package mcp

import (
	"context"
	"sort"
	"sync"
)

type VersionResult struct {
	Node    string
	Version string
	Error   error
}

type Mismatch struct {
	Node     string
	Got      string
	Expected string
	IsError  bool
}

func CheckVersions(ctx context.Context, clients map[string]*Client) []VersionResult {
	results := make([]VersionResult, 0, len(clients))
	ch := make(chan VersionResult, len(clients))

	var wg sync.WaitGroup
	for node, client := range clients {
		wg.Add(1)
		go func(node string, client *Client) {
			defer wg.Done()
			result, err := client.Initialize(ctx)
			if err != nil {
				ch <- VersionResult{Node: node, Error: err}
				return
			}
			ch <- VersionResult{Node: node, Version: result.ServerInfo.Version}
		}(node, client)
	}

	wg.Wait()
	close(ch)
	for result := range ch {
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Node < results[j].Node
	})
	return results
}

func CheckVersionMismatches(cliVersion string, results []VersionResult) []Mismatch {
	if cliVersion == "dev" {
		return nil
	}

	mismatches := make([]Mismatch, 0)
	for _, result := range results {
		if result.Error != nil {
			mismatches = append(mismatches, Mismatch{
				Node:     result.Node,
				Got:      result.Error.Error(),
				Expected: cliVersion,
				IsError:  true,
			})
			continue
		}
		if result.Version == cliVersion || result.Version == "dev" {
			continue
		}
		mismatches = append(mismatches, Mismatch{
			Node:     result.Node,
			Got:      result.Version,
			Expected: cliVersion,
		})
	}
	return mismatches
}
