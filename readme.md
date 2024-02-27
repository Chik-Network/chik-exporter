# Chik Exporter

Chik Exporter is an application that is intended to run alongside a chik installation and exports prometheus style metrics based on data available from Chik RPCs. Where possible, all data is received as events from websocket subscriptions. Some data that is not available as a metrics event is also fetched as well, but usually in response to an event that was already received that indicates the data may have changed (with the goal to only make as many RPC requests as necessary to get accurate metric data).

**_This project is actively under development and relies on data that may not yet be available in a stable release of Chik Blockchain. Dev builds of chik may contain bugs or other issues that are not present in tagged releases. We do not recommend that you run pre-release/dev versions of Chik Blockchain on mission critical systems._**

## Installation

Download the correct executable file from the release page and run. If you are on debian/ubuntu, you can install using the apt repo, documented below.

### Apt Repo Installation

#### Set up the repository

1. Update the `apt` package index and install packages to allow apt to use a repository over HTTPS:

```shell
sudo apt-get update

sudo apt-get install ca-certificates curl gnupg
```

2. Add Chik's official GPG Key:

```shell
curl -sL https://repo.chiknetwork.com/FD39E6D3.pubkey.asc | sudo gpg --dearmor -o /usr/share/keyrings/chik.gpg
```

3. Use the following command to set up the stable repository.

```shell 
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/chik.gpg] https://repo.chiknetwork.com/chik-exporter/debian/ stable main" | sudo tee /etc/apt/sources.list.d/chik-exporter.list > /dev/null
```

#### Install Chik Exporter

1. Update the apt package index and install the latest version of Chik Exporter

```shell
sudo apt-get update

sudo apt-get install chik-exporter
```

## Usage

First, install [chik-blockchain](https://github.com/Chik-Network/chik-blockchain). Chik exporter expects to be run on the same machine as the chik blockchain installation, and will use either the default chik config (`~/.chik/mainnet/`) or else the config located at `CHIK_ROOT`, if the environment variable is set.

`chik-exporter serve` will start the metrics exporter on the default port of `9914`. Metrics will be available at `<hostname>:9914/metrics`.

### Running in the background

To run Chik exporter in the background and have it automatically start when you boot your computer, you can create a `systemd` unit file. If you have installed with `apt` or from the `.deb` package, the systemd file is installed automatically and you can skip the downloading step below. 

Download [chik-exporter@.service](chik-exporter%40.service) and copy it to the `/etc/systemd/system/` folder on your machine.   

Save the file and start the service. Replace `[YOUR-USERNAME]` with the username of the user and group you want running the service. 
We assume that your username and group name are the same. 

```shell
sudo systemctl daemon-reload
sudo systemctl start chik-exporter@[YOUR-USERNAME].service
sudo systemctl status chik-exporter@[YOUR-USERNAME].service

```

The last command should show that the service is Running. 

To start chik-exporter at boot:

```shell
sudo systemctl enable chik-exporter@[YOUR-USERNAME].service
```

### Configuration

Configuration options can be passed using command line flags, environment variables, or a configuration file, except for `--config`, which is a CLI flag only. For a complete listing of options, run `chik-exporter --help`.

To set a config value as an environment variable, prefix the name with `CHIK_EXPORTER_`, convert all letters to uppercase, and replace any dashes with underscores (`metrics-port` becomes `CHIK_EXPORTER_METRICS_PORT`).

To use a config file, create a new yaml file and place any configuration options you want to specify in the file. The config file will be loaded by default from `~/.chik-exporter`, but the location can be overridden with the `--config` flag.

```yaml
metrics-port: 9914
```

## Country Data

When running alongside the crawler, the exporter can optionally export metrics indicating how many peers have been discovered in each country, based on IP address. To enable this functionality, you will need to download the MaxMind GeoLite2 Country database and provide the path to the MaxMind database to the exporter application. The path can be provided with a command line flag `--maxmind-db-path /path/to/GeoLite2-Country.mmdb`, an entry in the config yaml file `maxmind-db-path: /path/to/GeoLite2-Country.mmdb`, or an environment variable `CHIK_EXPORTER_MAXMIND_DB_PATH=/path/to/GeoLite2-Country.mmdb`. To gain access to the MaxMind DB, you can [register here](https://www.maxmind.com/en/geolite2/signup).
