package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/gauge"
	"github.com/grafana/grafana-foundation-sdk/go/logs"
	"github.com/grafana/grafana-foundation-sdk/go/loki"
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
		Description("Minecraft server metrics and logs").
		Tags([]string{"minecraft", "papermc", "kubernetes"}).
		Timezone("browser").
		Refresh("30s").
		WithVariable(dashboard.NewDatasourceVariableBuilder("datasource").
			Label("Prometheus").
			Type("prometheus")).
		WithVariable(dashboard.NewDatasourceVariableBuilder("loki").
			Label("Loki").
			Type("loki")).
		WithVariable(dashboard.NewQueryVariableBuilder("pod").
			Label("Pod").
			Datasource(datasourceRef("${datasource}")).
			Query(stringQuery("label_values(mc_tps, pod)")).
			Refresh(dashboard.VariableRefreshOnDashboardLoad)).
		// Row 1: Game Status
		WithPanel(
			gauge.NewPanelBuilder().
				Title("TPS").
				Description("Ticks per second (target: 20)").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("mc_tps{pod=~\"$pod\"}").
						LegendFormat("TPS"),
				).
				Min(0).
				Max(20).
				GridPos(gridPos(5, 4, 0, 0)),
		).
		WithPanel(
			stat.NewPanelBuilder().
				Title("Players Online").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(mc_players_online_total{pod=~\"$pod\"})").
						LegendFormat("Online"),
				).
				GridPos(gridPos(5, 4, 4, 0)),
		).
		WithPanel(
			stat.NewPanelBuilder().
				Title("Total Players").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("mc_players_total{pod=~\"$pod\"}").
						LegendFormat("Total"),
				).
				GridPos(gridPos(5, 4, 8, 0)),
		).
		WithPanel(
			stat.NewPanelBuilder().
				Title("Loaded Chunks").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(mc_loaded_chunks_total{pod=~\"$pod\"})").
						LegendFormat("Chunks"),
				).
				GridPos(gridPos(5, 4, 12, 0)),
		).
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
				GridPos(gridPos(5, 4, 16, 0)),
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
				GridPos(gridPos(5, 4, 20, 0)),
		).
		// Row 2: TPS and Tick Duration
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("TPS Over Time").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("mc_tps{pod=~\"$pod\"}").
						LegendFormat("TPS"),
				).
				Min(0).
				Max(20).
				GridPos(gridPos(8, 12, 0, 5)),
		).
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Tick Duration").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("mc_tick_duration_average{pod=~\"$pod\"} / 1000000").
						LegendFormat("Average"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("mc_tick_duration_median{pod=~\"$pod\"} / 1000000").
						LegendFormat("Median"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("mc_tick_duration_max{pod=~\"$pod\"} / 1000000").
						LegendFormat("Max"),
				).
				Unit("ms").
				GridPos(gridPos(8, 12, 12, 5)),
		).
		// Row 3: World Stats
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("World Size").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("mc_world_size{pod=~\"$pod\"}").
						LegendFormat("{{world}}"),
				).
				Unit("bytes").
				GridPos(gridPos(8, 12, 0, 13)),
		).
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Players per World").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("mc_players_online_total{pod=~\"$pod\"}").
						LegendFormat("{{world}}"),
				).
				GridPos(gridPos(8, 12, 12, 13)),
		).
		// Row 4: JVM Memory and GC
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("JVM Memory").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("mc_jvm_memory{pod=~\"$pod\", type=\"allocated\"}").
						LegendFormat("Allocated"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("mc_jvm_memory{pod=~\"$pod\", type=\"max\"}").
						LegendFormat("Max"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("mc_jvm_memory{pod=~\"$pod\", type=\"free\"}").
						LegendFormat("Free"),
				).
				Unit("bytes").
				GridPos(gridPos(8, 12, 0, 21)),
		).
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("JVM GC").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("rate(mc_jvm_gc_collection_seconds_sum{pod=~\"$pod\"}[5m])").
						LegendFormat("{{gc}}"),
				).
				Unit("s").
				GridPos(gridPos(8, 12, 12, 21)),
		).
		// Row 5: Container CPU and Memory
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Container CPU").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(rate(container_cpu_usage_seconds_total{pod=~\"$pod\", container=\"papermc\"}[5m]))").
						LegendFormat("CPU Usage"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("sum(container_spec_cpu_quota{pod=~\"$pod\", container=\"papermc\"} / container_spec_cpu_period{pod=~\"$pod\", container=\"papermc\"})").
						LegendFormat("Limit"),
				).
				Unit("short").
				GridPos(gridPos(8, 12, 0, 29)),
		).
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Container Memory").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("container_memory_working_set_bytes{pod=~\"$pod\", container=\"papermc\"}").
						LegendFormat("Working Set"),
				).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("container_spec_memory_limit_bytes{pod=~\"$pod\", container=\"papermc\"}").
						LegendFormat("Limit"),
				).
				Unit("bytes").
				GridPos(gridPos(8, 12, 12, 29)),
		).
		// Row 6: Network and Disk
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
				GridPos(gridPos(8, 12, 0, 37)),
		).
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
				GridPos(gridPos(8, 12, 12, 37)),
		).
		// Row 7: Logs
		WithPanel(
			logs.NewPanelBuilder().
				Title("Server Logs").
				Datasource(datasourceRef("${loki}")).
				ShowTime(true).
				WrapLogMessage(true).
				WithTarget(
					loki.NewDataqueryBuilder().
						Expr(`{kubernetes_namespace_name="paper", kubernetes_pod_name=~"$pod"}`),
				).
				GridPos(gridPos(12, 24, 0, 45)),
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
