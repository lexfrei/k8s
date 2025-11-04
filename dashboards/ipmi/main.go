package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

func main() {
	// Helper to create DataSourceRef
	datasourceRef := func(uid string) dashboard.DataSourceRef {
		ds := dashboard.NewDataSourceRef()
		ds.Uid = &uid
		return *ds
	}

	// Helper to create StringOrMap from string
	stringQuery := func(s string) dashboard.StringOrMap {
		q := dashboard.NewStringOrMap()
		q.String = &s
		return *q
	}

	// Helper to create GridPos
	gridPos := func(h, w, x, y uint32) dashboard.GridPos {
		gp := dashboard.NewGridPos()
		gp.H = h
		gp.W = w
		gp.X = x
		gp.Y = y
		return *gp
	}

	// Helper to create ValueMappingResult
	valueMappingResult := func(text, color string) dashboard.ValueMappingResult {
		r := dashboard.NewValueMappingResult()
		r.Text = &text
		r.Color = &color
		return *r
	}

	// Build dashboard
	builder := dashboard.NewDashboardBuilder("IPMI Hardware Monitoring").
		Description("Hardware metrics from prometheus-ipmi-exporter").
		Tags([]string{"ipmi", "hardware", "monitoring"}).
		Timezone("browser").
		Refresh("1m").
		// Add datasource variable
		WithVariable(dashboard.NewDatasourceVariableBuilder("datasource").
			Label("Datasource")).
		// Add instance variable
		WithVariable(dashboard.NewQueryVariableBuilder("instance").
			Label("Instance").
			Datasource(datasourceRef("${datasource}")).
			Query(stringQuery("label_values(ipmi_bmc_info, instance)")).
			Multi(true).
			IncludeAll(true).
			Refresh(dashboard.VariableRefreshOnDashboardLoad)).
		// BMC Firmware panel
		WithPanel(
			stat.NewPanelBuilder().
				Title("BMC Firmware").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("ipmi_bmc_info{instance=~\"$instance\"}").
						LegendFormat("{{firmware_revision}}"),
				).
				GridPos(gridPos(4, 6, 0, 1)),
		).
		// Power State panel
		WithPanel(
			stat.NewPanelBuilder().
				Title("Power State").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("ipmi_chassis_power_state{instance=~\"$instance\"}"),
				).
				Mappings([]dashboard.ValueMapping{
					{
						ValueMap: &dashboard.ValueMap{
							Type: dashboard.MappingTypeValueToText,
							Options: map[string]dashboard.ValueMappingResult{
								"1": valueMappingResult("ON", "green"),
								"0": valueMappingResult("OFF", "red"),
							},
						},
					},
				}).
				GridPos(gridPos(4, 6, 6, 1)),
		).
		// Temperatures panel
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Temperatures").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("ipmi_temperature_celsius{instance=~\"$instance\"}").
						LegendFormat("{{name}}"),
				).
				Unit("celsius").
				GridPos(gridPos(8, 24, 0, 6)),
		).
		// Fan Speeds (RPM) panel
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Fan Speeds (RPM)").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("ipmi_fan_speed_rpm{instance=~\"$instance\"}").
						LegendFormat("{{name}}"),
				).
				Unit("rpm").
				GridPos(gridPos(8, 12, 0, 14)),
		).
		// Fan Speed Ratio panel
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Fan Speed Ratio").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("ipmi_fan_speed_ratio{instance=~\"$instance\"}").
						LegendFormat("{{name}}"),
				).
				Unit("percentunit").
				Min(0).
				Max(1).
				GridPos(gridPos(8, 12, 12, 14)),
		).
		// Voltage panel
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Voltage").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("ipmi_voltage_volts{instance=~\"$instance\"}").
						LegendFormat("{{name}}"),
				).
				Unit("volt").
				GridPos(gridPos(8, 12, 0, 22)),
		).
		// Power Consumption panel
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Power Consumption (DCMI)").
				Datasource(datasourceRef("${datasource}")).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr("ipmi_dcmi_power_consumption_watts{instance=~\"$instance\"}").
						LegendFormat("Current"),
				).
				Unit("watt").
				GridPos(gridPos(8, 12, 12, 22)),
		)

	// Build dashboard
	dash, err := builder.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building dashboard: %v\n", err)
		os.Exit(1)
	}

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(dash, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	// Write to file or stdout
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
