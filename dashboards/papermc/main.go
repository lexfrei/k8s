package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/gauge"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

func main() {
	datasourceRef := func(uid string) dashboard.DataSourceRef {
		ds := dashboard.NewDataSourceRef()
		ds.Uid = &uid
		return *ds
	}

	stringQuery := func(s string) dashboard.StringOrMap {
		q := dashboard.NewStringOrMap()
		q.String = &s
		return *q
	}

	gridPos := func(h, w, x, y uint32) dashboard.GridPos {
		gp := dashboard.NewGridPos()
		gp.H = h
		gp.W = w
		gp.X = x
		gp.Y = y
		return *gp
	}

	valueMappingResult := func(text, color string) dashboard.ValueMappingResult {
		r := dashboard.NewValueMappingResult()
		r.Text = &text
		r.Color = &color
		return *r
	}

	builder := dashboard.NewDashboardBuilder("PaperMC Server Monitoring").
		Description("Container metrics for PaperMC Minecraft server").
		Tags([]string{"minecraft", "papermc", "kubernetes", "container"}).
		Timezone("browser").
		Refresh("30s").
		WithVariable(dashboard.NewDatasourceVariableBuilder("datasource").
			Label("Datasource").
			Type("prometheus")).
		WithVariable(dashboard.NewQueryVariableBuilder("pod").
			Label("Pod").
			Datasource(datasourceRef("${datasource}")).
			Query(stringQuery("label_values(container_cpu_usage_seconds_total{container=\"papermc\"}, pod)")).
			Refresh(dashboard.VariableRefreshOnDashboardLoad)).
		// Row 1: Status panels
		WithPanel(
			stat.NewPanelBuilder().
				Title("Status").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("count(container_last_seen{pod=~\"$pod\", container=\"papermc\"} > (time() - 60))").
						LegendFormat("Status"),
				).
				Mappings([]dashboard.ValueMapping{
					{
						ValueMap: &dashboard.ValueMap{
							Type: dashboard.MappingTypeValueToText,
							Options: map[string]dashboard.ValueMappingResult{
								"1": valueMappingResult("UP", "green"),
								"0": valueMappingResult("DOWN", "red"),
							},
						},
					},
				}).
				GridPos(gridPos(4, 6, 0, 0)),
		).
		WithPanel(
			stat.NewPanelBuilder().
				Title("CPU Usage").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(rate(container_cpu_usage_seconds_total{pod=~\"$pod\", container=\"papermc\"}[5m])) / sum(container_spec_cpu_quota{pod=~\"$pod\", container=\"papermc\"} / container_spec_cpu_period{pod=~\"$pod\", container=\"papermc\"}) * 100").
						LegendFormat("CPU %"),
				).
				Unit("percent").
				GridPos(gridPos(4, 6, 6, 0)),
		).
		WithPanel(
			stat.NewPanelBuilder().
				Title("Memory Usage").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(container_memory_working_set_bytes{pod=~\"$pod\", container=\"papermc\"}) / sum(container_spec_memory_limit_bytes{pod=~\"$pod\", container=\"papermc\"}) * 100").
						LegendFormat("Memory %"),
				).
				Unit("percent").
				GridPos(gridPos(4, 6, 12, 0)),
		).
		WithPanel(
			stat.NewPanelBuilder().
				Title("Uptime").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("time() - container_start_time_seconds{pod=~\"$pod\", container=\"papermc\"}").
						LegendFormat("Uptime"),
				).
				Unit("s").
				GridPos(gridPos(4, 6, 18, 0)),
		).
		// Row 2: CPU panels
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("CPU Usage").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(rate(container_cpu_usage_seconds_total{pod=~\"$pod\", container=\"papermc\"}[5m]))").
						LegendFormat("CPU cores"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(container_spec_cpu_quota{pod=~\"$pod\", container=\"papermc\"} / container_spec_cpu_period{pod=~\"$pod\", container=\"papermc\"})").
						LegendFormat("Limit"),
				).
				Unit("short").
				GridPos(gridPos(8, 12, 0, 4)),
		).
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("CPU Throttling").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("rate(container_cpu_cfs_throttled_seconds_total{pod=~\"$pod\", container=\"papermc\"}[5m])").
						LegendFormat("Throttled seconds/s"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("rate(container_cpu_cfs_throttled_periods_total{pod=~\"$pod\", container=\"papermc\"}[5m]) / rate(container_cpu_cfs_periods_total{pod=~\"$pod\", container=\"papermc\"}[5m]) * 100").
						LegendFormat("Throttled periods %"),
				).
				Unit("short").
				GridPos(gridPos(8, 12, 12, 4)),
		).
		// Row 3: Memory panels
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Memory Usage").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("container_memory_working_set_bytes{pod=~\"$pod\", container=\"papermc\"}").
						LegendFormat("Working Set"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("container_memory_rss{pod=~\"$pod\", container=\"papermc\"}").
						LegendFormat("RSS"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("container_memory_cache{pod=~\"$pod\", container=\"papermc\"}").
						LegendFormat("Cache"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("container_spec_memory_limit_bytes{pod=~\"$pod\", container=\"papermc\"}").
						LegendFormat("Limit"),
				).
				Unit("bytes").
				GridPos(gridPos(8, 12, 0, 12)),
		).
		WithPanel(
			gauge.NewPanelBuilder().
				Title("Memory vs Limit").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(container_memory_working_set_bytes{pod=~\"$pod\", container=\"papermc\"}) / sum(container_spec_memory_limit_bytes{pod=~\"$pod\", container=\"papermc\"}) * 100").
						LegendFormat("Memory %"),
				).
				Unit("percent").
				Min(0).
				Max(100).
				GridPos(gridPos(8, 12, 12, 12)),
		).
		// Row 4: Network panels
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Network Traffic").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(rate(container_network_receive_bytes_total{pod=~\"$pod\"}[5m]))").
						LegendFormat("Receive"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(rate(container_network_transmit_bytes_total{pod=~\"$pod\"}[5m]))").
						LegendFormat("Transmit"),
				).
				Unit("Bps").
				GridPos(gridPos(8, 12, 0, 20)),
		).
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Network Packets").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(rate(container_network_receive_packets_total{pod=~\"$pod\"}[5m]))").
						LegendFormat("Receive pps"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(rate(container_network_transmit_packets_total{pod=~\"$pod\"}[5m]))").
						LegendFormat("Transmit pps"),
				).
				Unit("pps").
				GridPos(gridPos(8, 12, 12, 20)),
		).
		// Row 5: Disk I/O panels
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Disk I/O").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(rate(container_fs_reads_bytes_total{pod=~\"$pod\", container=\"papermc\"}[5m]))").
						LegendFormat("Read"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(rate(container_fs_writes_bytes_total{pod=~\"$pod\", container=\"papermc\"}[5m]))").
						LegendFormat("Write"),
				).
				Unit("Bps").
				GridPos(gridPos(8, 12, 0, 28)),
		).
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Disk IOPS").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(rate(container_fs_reads_total{pod=~\"$pod\", container=\"papermc\"}[5m]))").
						LegendFormat("Read IOPS"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(rate(container_fs_writes_total{pod=~\"$pod\", container=\"papermc\"}[5m]))").
						LegendFormat("Write IOPS"),
				).
				Unit("iops").
				GridPos(gridPos(8, 12, 12, 28)),
		).
		// Row 6: System panels
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Threads").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("container_threads{pod=~\"$pod\", container=\"papermc\"}").
						LegendFormat("Threads"),
				).
				Unit("short").
				GridPos(gridPos(6, 8, 0, 36)),
		).
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("File Descriptors").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("container_file_descriptors{pod=~\"$pod\", container=\"papermc\"}").
						LegendFormat("Open FDs"),
				).
				Unit("short").
				GridPos(gridPos(6, 8, 8, 36)),
		).
		WithPanel(
			stat.NewPanelBuilder().
				Title("OOM Events").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(container_oom_events_total{pod=~\"$pod\", container=\"papermc\"})").
						LegendFormat("OOM Events"),
				).
				Unit("short").
				GridPos(gridPos(6, 8, 16, 36)),
		)

	dash, err := builder.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building dashboard: %v\n", err)
		os.Exit(1)
	}

	jsonData, err := json.MarshalIndent(dash, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		err = os.WriteFile(os.Args[1], jsonData, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Dashboard written to %s\n", os.Args[1])
	} else {
		fmt.Println(string(jsonData))
	}
}
