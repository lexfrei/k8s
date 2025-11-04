# IPMI Hardware Monitoring Dashboard

Grafana dashboard for visualizing IPMI hardware metrics from `prometheus-ipmi-exporter`.

Built with **Grafana Foundation SDK** (Go).

## Features

- **Datasource variable**: Easily switch between VictoriaMetrics datasources
- **Multi-instance support**: Template variable for filtering by instance(s)
- **BMC Information**: Firmware version, power state
- **Temperatures**: All temperature sensors over time
- **Fan Monitoring**: RPM and speed ratio
- **Power Metrics**: Voltage sensors, DCMI power consumption
- **Type-safe**: Built with Grafana Foundation SDK for Go

## Requirements

- Go 1.24+
- Grafana Foundation SDK

## Usage

### Generate Dashboard JSON

```bash
# Install dependencies
go mod download

# Generate dashboard JSON
go run main.go ipmi-dashboard.json
```

This creates `ipmi-dashboard.json` that can be imported into Grafana.

### Apply to Kubernetes

The dashboard is automatically deployed via GrafanaDashboard CRD in ArgoCD.

```bash
# Manual apply (for testing)
kubectl apply --filename ../../manifests/ipmi-exporter/grafana-dashboard.yaml
```

### Development

Edit `main.go` and regenerate:

```bash
go run main.go ipmi-dashboard.json
```

## Dashboard Panels

1. **BMC Firmware** - Shows firmware version
2. **Power State** - ON/OFF indicator with color mapping
3. **Temperatures** - All temperature sensors over time
4. **Fan Speeds (RPM)** - Fan speeds in RPM
5. **Fan Speed Ratio** - Fan speed percentage (0-100%)
6. **Voltage** - Voltage sensor readings
7. **Power Consumption (DCMI)** - DCMI power consumption in watts

## Variables

- **datasource**: VictoriaMetrics datasource selector
- **instance**: Multi-select instance filter (supports "All")

## Metrics Reference

Based on `prometheus-ipmi-exporter` metrics:

- `ipmi_bmc_info` - BMC information
- `ipmi_chassis_power_state` - Chassis power state (1=ON, 0=OFF)
- `ipmi_temperature_celsius` - Temperature sensors
- `ipmi_fan_speed_rpm` - Fan speeds in RPM
- `ipmi_fan_speed_ratio` - Fan speed ratio (0-1)
- `ipmi_voltage_volts` - Voltage sensors
- `ipmi_dcmi_power_consumption_watts` - DCMI power consumption

## Customization

Modify `main.go` to add/remove panels or change queries. The dashboard uses **Grafana Foundation SDK** for type-safe programmatic generation.

## Architecture

Uses **Grafana Foundation SDK for Go** (not deprecated grafanalib):
- Type-safe builders
- IDE autocomplete support
- Compile-time validation
- Part of official Grafana "Observability as Code" initiative
