package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	pb "github.com/srujankothuri/SentinelFS/proto"
)

// WatchCluster continuously displays cluster health, refreshing every interval
func (c *Client) WatchCluster(interval time.Duration) error {
	fmt.Println("Watching cluster health (Ctrl+C to stop)...")
	fmt.Println()

	for {
		// Clear screen
		fmt.Print("\033[H\033[2J")

		fmt.Println("═══════════════════════════════════════════════════════════════════")
		fmt.Println("              SentinelFS 🛡️  Live Cluster Monitor                 ")
		fmt.Printf("              %s\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Println("═══════════════════════════════════════════════════════════════════")
		fmt.Println()

		// Cluster overview
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		clusterResp, err := c.meta.GetClusterInfo(ctx, &pb.GetClusterInfoRequest{})
		cancel()

		if err != nil {
			fmt.Printf("  ⚠️  Error fetching cluster info: %v\n", err)
			time.Sleep(interval)
			continue
		}

		fmt.Printf("  Nodes:   %d total  │  🟢 %d healthy  🟡 %d warning  🟠 %d at-risk  💀 %d dead\n",
			clusterResp.TotalNodes,
			clusterResp.HealthyNodes,
			clusterResp.WarningNodes,
			clusterResp.AtRiskNodes,
			clusterResp.DeadNodes,
		)
		fmt.Printf("  Storage: %s / %s\n",
			formatSize(clusterResp.TotalUsed),
			formatSize(clusterResp.TotalCapacity),
		)
		fmt.Printf("  Files:   %d  │  Chunks: %d\n",
			clusterResp.TotalFiles,
			clusterResp.TotalChunks,
		)
		fmt.Println()

		// Node details
		ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
		nodeResp, err := c.meta.GetNodeInfo(ctx2, &pb.GetNodeInfoRequest{})
		cancel2()

		if err != nil {
			fmt.Printf("  ⚠️  Error fetching node info: %v\n", err)
			time.Sleep(interval)
			continue
		}

		fmt.Println("  ┌──────────┬──────────────────────┬──────────┬────────────────────┬────────┐")
		fmt.Println("  │ NODE     │ ADDRESS              │ STATUS   │ RISK               │ CHUNKS │")
		fmt.Println("  ├──────────┼──────────────────────┼──────────┼────────────────────┼────────┤")

		for _, n := range nodeResp.Nodes {
			icon := statusColor(n.Status)
			riskBar := renderRiskBar(n.RiskScore, 15)
			fmt.Printf("  │ %-8s │ %-20s │ %s %-7s │ %s %4.1f%% │ %6d │\n",
				n.NodeId,
				n.Address,
				icon,
				n.Status,
				riskBar,
				n.RiskScore*100,
				n.ChunkCount,
			)
		}

		fmt.Println("  └──────────┴──────────────────────┴──────────┴────────────────────┴────────┘")
		fmt.Println()
		fmt.Printf("  Refreshing every %s... (Ctrl+C to stop)\n", interval)

		time.Sleep(interval)
	}
}

// renderRiskBar creates a visual bar showing risk level
func renderRiskBar(risk float64, width int) string {
	filled := int(risk * float64(width))
	if filled > width {
		filled = width
	}

	var bar strings.Builder

	for i := 0; i < width; i++ {
		if i < filled {
			switch {
			case risk >= 0.8:
				bar.WriteString("🟥")
			case risk >= 0.6:
				bar.WriteString("🟧")
			case risk >= 0.3:
				bar.WriteString("🟨")
			default:
				bar.WriteString("🟩")
			}
		} else {
			bar.WriteString("░")
		}
	}

	return bar.String()
}