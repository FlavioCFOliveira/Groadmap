package commands

import (
	"fmt"
	"sort"

	"github.com/FlavioCFOliveira/Groadmap/internal/metrics"
)

// HandleMetrics handles metrics commands.
func HandleMetrics(args []string) error {
	if len(args) == 0 {
		return metricsShow()
	}

	subcommand := args[0]

	switch subcommand {
	case "show", "list":
		return metricsShow()
	case "reset":
		return metricsReset()
	case "enable":
		return metricsEnable()
	case "disable":
		return metricsDisable()
	case "status":
		return metricsStatus()
	default:
		return fmt.Errorf("unknown metrics subcommand: %s", subcommand)
	}
}

// metricsShow displays current metrics.
func metricsShow() error {
	summary := metrics.GetSummary()

	if len(summary.Operations) == 0 && len(summary.Counters) == 0 {
		fmt.Println("No metrics collected yet.")
		fmt.Println("Metrics are collected automatically during operations.")
		return nil
	}

	// Display operation metrics
	if len(summary.Operations) > 0 {
		fmt.Println("=== Operation Metrics ===")
		fmt.Printf("%-30s %8s %12s %12s %12s\n", "Operation", "Count", "Avg", "Min", "Max")
		fmt.Println(string(make([]byte, 80)))

		// Sort by name for consistent output
		sort.Slice(summary.Operations, func(i, j int) bool {
			return summary.Operations[i].Name < summary.Operations[j].Name
		})

		for _, op := range summary.Operations {
			fmt.Printf("%-30s %8d %12s %12s %12s\n",
				op.Name,
				op.Count,
				metrics.FormatDuration(op.AvgTime),
				metrics.FormatDuration(op.MinTime),
				metrics.FormatDuration(op.MaxTime),
			)
		}
		fmt.Println()
	}

	// Display counter metrics
	if len(summary.Counters) > 0 {
		fmt.Println("=== Counter Metrics ===")
		fmt.Printf("%-30s %10s\n", "Counter", "Value")
		fmt.Println(string(make([]byte, 50)))

		// Sort by name
		sort.Slice(summary.Counters, func(i, j int) bool {
			return summary.Counters[i].Name < summary.Counters[j].Name
		})

		for _, counter := range summary.Counters {
			fmt.Printf("%-30s %10d\n", counter.Name, counter.Value)
		}
		fmt.Println()
	}

	return nil
}

// metricsReset clears all metrics.
func metricsReset() error {
	metrics.Reset()
	fmt.Println("Metrics reset successfully.")
	return nil
}

// metricsEnable enables metrics collection.
func metricsEnable() error {
	metrics.Enable()
	fmt.Println("Metrics collection enabled.")
	return nil
}

// metricsDisable disables metrics collection.
func metricsDisable() error {
	metrics.Disable()
	fmt.Println("Metrics collection disabled.")
	return nil
}

// metricsStatus shows metrics collection status.
func metricsStatus() error {
	if metrics.IsEnabled() {
		fmt.Println("Metrics collection: ENABLED")
	} else {
		fmt.Println("Metrics collection: DISABLED")
	}

	summary := metrics.GetSummary()
	fmt.Printf("Operations tracked: %d\n", len(summary.Operations))
	fmt.Printf("Counters tracked: %d\n", len(summary.Counters))

	return nil
}

// printMetricsHelp prints help for metrics commands.
func printMetricsHelp() {
	fmt.Println("Usage: rmp metrics [command]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  show, list    Show current metrics")
	fmt.Println("  reset         Reset all metrics")
	fmt.Println("  enable        Enable metrics collection")
	fmt.Println("  disable       Disable metrics collection")
	fmt.Println("  status        Show metrics status")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  rmp metrics              # Show current metrics")
	fmt.Println("  rmp metrics show         # Show current metrics")
	fmt.Println("  rmp metrics reset        # Reset all metrics")
	fmt.Println("  rmp metrics status       # Show collection status")
}
