package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	chatpb "github.com/qtopie/domour/gen/assistant/chat"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const renderedArtifactMarker = "Motor rendered the artifact below:\n"

func main() {
	addr := flag.String("addr", "127.0.0.1:1234", "Domour gRPC address")
	sessionID := flag.String("session", "domour-chat-cli", "Chat session ID")
	workspace := flag.String("workspace", ".", "Workspace to send with the chat request")
	message := flag.String("message", "请画一个大脑架构图，展示 cortex、hippocampus、amygdala、brainstem 和 sensory input / motor output 的关系，并渲染成 html 网页。", "Chat prompt")
	outputPath := flag.String("out", "", "Path to save the rendered artifact")
	timeout := flag.Duration("timeout", 2*time.Minute, "Overall request timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, *addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		log.Fatalf("dial domour: %v", err)
	}
	defer conn.Close()

	client := chatpb.NewChatServiceClient(conn)
	stream, err := client.Chat(ctx, &chatpb.ChatRequest{
		SessionId: *sessionID,
		Seq:       1,
		Workspace: *workspace,
		Message:   *message,
	})
	if err != nil {
		log.Fatalf("chat request failed: %v", err)
	}

	var finalContent string
	var finalMeta map[string]string
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("read chat stream: %v", err)
		}

		fmt.Printf("stage=%s format=%s provider=%s chunkSeq=%d maxChecksum=%d\n",
			resp.GetMeta()["stage"], resp.GetMeta()["format"], resp.GetMeta()["provider"],
			resp.GetChunkSeq(), resp.GetMaxSeqChecksum())
		fmt.Println(trimForDisplay(resp.GetContent(), 240))
		fmt.Println("---")

		finalContent = resp.GetContent()
		finalMeta = resp.GetMeta()
	}

	if strings.TrimSpace(finalContent) == "" {
		log.Fatal("chat completed without final content")
	}

	rendered := extractRenderedArtifact(finalContent)
	if strings.TrimSpace(rendered) == "" {
		log.Fatal("final chat response did not contain a rendered artifact")
	}

	targetPath := strings.TrimSpace(*outputPath)
	if targetPath == "" {
		targetPath = defaultOutputPath(finalMeta["format"])
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		log.Fatalf("create output directory: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte(rendered), 0o644); err != nil {
		log.Fatalf("write rendered artifact: %v", err)
	}

	fmt.Printf("saved=%s\n", targetPath)
}

func trimForDisplay(content string, max int) string {
	content = strings.TrimSpace(content)
	if len(content) <= max {
		return content
	}
	return content[:max] + "..."
}

func extractRenderedArtifact(content string) string {
	idx := strings.Index(content, renderedArtifactMarker)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(content[idx+len(renderedArtifactMarker):])
}

func defaultOutputPath(format string) string {
	filename := "brain-architecture"
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "html":
		return filepath.Join("build", filename+".html")
	default:
		return filepath.Join("build", filename+".svg")
	}
}
