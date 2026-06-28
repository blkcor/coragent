// Package main demonstrates the coragent model backend streaming.
// Run against a real endpoint or the fake provider.
//
// TODO: This binary imports internal/ packages directly because pkg/agent
// does not yet expose provider/config factories. Once the SDK surface is
// complete, migrate to importing only pkg/agent (invariant #2).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/blkcor/coragent/internal/config"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/provider/testutil"
	"github.com/blkcor/coragent/pkg/agent"
)

func main() {
	// Determine mode from command-line argument
	useFake := false
	if len(os.Args) > 1 && os.Args[1] == "fake" {
		useFake = true
	}

	var p agent.Provider

	if useFake {
		// Use fake provider with scripted replies
		p = testutil.NewFakeProvider([]testutil.ScriptedReply{
			{
				TextDeltas: []string{"Hello", " from", " the", " fake", " provider!"},
				EndReason:  agent.Finished,
			},
		})
		fmt.Println("Using fake provider (no credentials needed)")
	} else {
		// Load settings and use real provider
		settings, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load settings: %v\n", err)
			os.Exit(1)
		}

		if settings.Model == nil {
			fmt.Fprintf(os.Stderr, "No model settings configured\n")
			os.Exit(1)
		}

		p = provider.NewOpenAIProvider(
			settings.Model.BaseURL,
			settings.Model.APIKey,
			settings.Model.Name,
		)
		fmt.Printf("Using real endpoint: %s\n", settings.Model.BaseURL)
	}

	// Create a sample conversation
	conv := agent.Conversation{
		Turns: []agent.Turn{
			{Role: "user", Content: "Hello, how are you?"},
		},
	}

	// Define sample tools
	tools := []agent.Tool{
		{
			Name:        "read_file",
			Description: "Read a file from the filesystem",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
	}

	// Stream reply
	fmt.Println("\n--- Streaming reply ---")
	events := p.StreamReply(context.Background(), conv, tools, agent.StreamOptions{})

	var fullText string
	var toolCalls []agent.ToolCall
	var endReason agent.ReplyEndReason

	for event := range events {
		switch event.Type {
		case agent.TextDelta:
			fmt.Print(event.TextDelta)
			fullText += event.TextDelta
		case agent.ToolCallEvent:
			if event.ToolCall != nil {
				toolCalls = append(toolCalls, *event.ToolCall)
				fmt.Printf("\n[Tool Call: %s(%v)]\n", event.ToolCall.ToolName, event.ToolCall.Arguments)
			}
		case agent.ReplyEndedEvent:
			if event.ReplyEnded != nil {
				endReason = event.ReplyEnded.Reason
			}
		case agent.ErrorEvent:
			fmt.Fprintf(os.Stderr, "\n[Error: %v]\n", event.Error)
			os.Exit(1)
		}
	}

	fmt.Println("\n--- End of reply ---")
	fmt.Printf("Reply ended: ")
	switch endReason {
	case agent.Finished:
		fmt.Println("finished normally")
	case agent.StoppedToCallTools:
		fmt.Println("stopped to call tools")
	case agent.CutOff:
		fmt.Println("cut off at length limit")
	}

	if len(toolCalls) > 0 {
		fmt.Printf("Tool calls requested: %d\n", len(toolCalls))
	}
}
