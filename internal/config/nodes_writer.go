package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pockyHM/conan/pkg/configschema"
	"gopkg.in/yaml.v3"
)

type NodeWriter struct {
	home string
}

type WriteNodeOptions struct {
	Update      bool
	RotateToken bool
}

type WriteNodeResult struct {
	Node    configschema.NodeConfig
	Updated bool
}

func NewNodeWriter(home string) *NodeWriter {
	if home == "" {
		home = DefaultHome()
	}
	return &NodeWriter{home: home}
}

func (w *NodeWriter) WriteNode(clusterName string, node configschema.NodeConfig, opts WriteNodeOptions) (WriteNodeResult, error) {
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" {
		return WriteNodeResult{}, fmt.Errorf("cluster name is required")
	}
	node.Name = strings.TrimSpace(node.Name)
	if node.Name == "" {
		return WriteNodeResult{}, fmt.Errorf("node name is required")
	}
	node.Host = strings.TrimSpace(node.Host)
	if node.Host == "" {
		return WriteNodeResult{}, fmt.Errorf("node host is required")
	}

	clusterDir := filepath.Join(w.home, "clusters", clusterName)
	clusterPath := filepath.Join(clusterDir, "cluster.yaml")
	if _, err := os.Stat(clusterPath); err != nil {
		if !os.IsNotExist(err) {
			return WriteNodeResult{}, err
		}
		if err := os.MkdirAll(clusterDir, 0755); err != nil {
			return WriteNodeResult{}, err
		}
		if err := writeClusterYAML(clusterPath, clusterName); err != nil {
			return WriteNodeResult{}, err
		}
	}

	nodesPath := filepath.Join(clusterDir, "nodes.yaml")
	var nodes configschema.NodeList
	if err := readYAMLIfExists(nodesPath, &nodes); err != nil {
		return WriteNodeResult{}, err
	}

	for i := range nodes.Nodes {
		if nodes.Nodes[i].Name != node.Name {
			continue
		}
		if !opts.Update {
			return WriteNodeResult{}, fmt.Errorf("node already exists: %s", node.Name)
		}
		if nodes.Nodes[i].Agent != nil && nodes.Nodes[i].Agent.Token != "" && !opts.RotateToken {
			if node.Agent == nil {
				node.Agent = &configschema.NodeAgentOverride{}
			}
			node.Agent.Token = nodes.Nodes[i].Agent.Token
		}
		nodes.Nodes[i] = node
		if err := writeNodeList(nodesPath, nodes); err != nil {
			return WriteNodeResult{}, err
		}
		return WriteNodeResult{Node: node, Updated: true}, nil
	}

	nodes.Nodes = append(nodes.Nodes, node)
	if err := writeNodeList(nodesPath, nodes); err != nil {
		return WriteNodeResult{}, err
	}
	return WriteNodeResult{Node: node, Updated: false}, nil
}

func (w *NodeWriter) AddCommandWhitelist(clusterName, nodeName, command string) error {
	clusterName = strings.TrimSpace(clusterName)
	nodeName = strings.TrimSpace(nodeName)
	command = strings.TrimSpace(command)
	if clusterName == "" {
		return fmt.Errorf("cluster name is required")
	}
	if nodeName == "" {
		return fmt.Errorf("node name is required")
	}
	if command == "" {
		return fmt.Errorf("command is required")
	}

	nodesPath := filepath.Join(w.home, "clusters", clusterName, "nodes.yaml")
	var nodes configschema.NodeList
	if err := readYAMLIfExists(nodesPath, &nodes); err != nil {
		return err
	}
	for i := range nodes.Nodes {
		if nodes.Nodes[i].Name != nodeName {
			continue
		}
		for _, existing := range nodes.Nodes[i].CommandWhitelist {
			if strings.TrimSpace(existing) == command {
				return writeNodeList(nodesPath, nodes)
			}
		}
		nodes.Nodes[i].CommandWhitelist = append(nodes.Nodes[i].CommandWhitelist, command)
		return writeNodeList(nodesPath, nodes)
	}
	return fmt.Errorf("node not found: %s", nodeName)
}

func (w *NodeWriter) AddLocalFileWhitelist(path string) error {
	path = strings.TrimSpace(filepath.ToSlash(filepath.Clean(path)))
	if path == "" || path == "." || path == ".." || strings.HasPrefix(path, "../") || strings.HasPrefix(path, "/") {
		return fmt.Errorf("local file path must be relative to workspace")
	}
	loader := NewLoader(w.home)
	cfg, err := loader.LoadGlobal()
	if err != nil {
		return err
	}
	for _, existing := range cfg.Security.LocalFileWhitelist {
		if strings.TrimSpace(filepath.ToSlash(filepath.Clean(existing))) == path {
			return loader.SaveGlobal(cfg)
		}
	}
	cfg.Security.LocalFileWhitelist = append(cfg.Security.LocalFileWhitelist, path)
	return loader.SaveGlobal(cfg)
}

func writeNodeList(path string, nodes configschema.NodeList) error {
	data, err := yaml.Marshal(nodes)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func writeClusterYAML(path string, name string) error {
	data, err := yaml.Marshal(configschema.ClusterConfig{Name: name})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
