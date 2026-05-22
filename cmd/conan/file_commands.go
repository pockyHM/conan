package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/spf13/cobra"
)

func newFilesCommand(home *string, clusterName *string) *cobra.Command {
	filesCmd := &cobra.Command{Use: "files", Short: "Transfer files between conan and agents"}
	filesCmd.AddCommand(&cobra.Command{
		Use:   "put <node> <local-path> <remote-path>",
		Short: "Upload a local file to an agent",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster, err := cfgloader.NewLoader(*home).LoadCluster(*clusterName)
			if err != nil {
				return err
			}
			nodeName, localPath, remotePath := args[0], args[1], args[2]
			info, err := os.Stat(localPath)
			if err != nil {
				return err
			}
			if info.IsDir() {
				return fmt.Errorf("local path is a directory: %s", localPath)
			}
			return forEachNode(cmd.Context(), cluster, []string{nodeName}, func(ctx context.Context, node cfgloader.Node) error {
				if err := uploadFile(ctx, node, localPath, remotePath); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\tuploaded\t%s\t%d bytes\n", node.Name, remotePath, info.Size())
				return nil
			})
		},
	})
	filesCmd.AddCommand(&cobra.Command{
		Use:   "get <node> <remote-path> <local-path>",
		Short: "Download a file from an agent",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster, err := cfgloader.NewLoader(*home).LoadCluster(*clusterName)
			if err != nil {
				return err
			}
			nodeName, remotePath, localPath := args[0], args[1], args[2]
			return forEachNode(cmd.Context(), cluster, []string{nodeName}, func(ctx context.Context, node cfgloader.Node) error {
				bytesWritten, err := downloadFile(ctx, node, remotePath, localPath)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\tdownloaded\t%s\t%d bytes\n", node.Name, remotePath, bytesWritten)
				return nil
			})
		},
	})
	return filesCmd
}

func uploadFile(ctx context.Context, node cfgloader.Node, localPath, remotePath string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	client := mcp.NewClient(mcp.Config{BaseURL: mcp.URL(node.Agent.Host, node.Agent.Port, node.Agent.TLS), Token: node.Agent.Token})
	_, err = client.UploadFile(ctx, remotePath, file)
	return err
}

func downloadFile(ctx context.Context, node cfgloader.Node, remotePath, localPath string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return 0, err
	}
	file, err := os.OpenFile(localPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	client := mcp.NewClient(mcp.Config{BaseURL: mcp.URL(node.Agent.Host, node.Agent.Port, node.Agent.TLS), Token: node.Agent.Token})
	return client.DownloadFile(ctx, remotePath, file)
}
