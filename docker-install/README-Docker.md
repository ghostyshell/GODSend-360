# GODSend Docker Setup

Run the GODSend Xbox 360 Game Server in a Docker container.

## Quick Start

### Option 1: Build from Source (Recommended)

```bash
# Clone the repository
git clone https://gitgud.io/Nesquin/godsend-homelab-edition.git
cd godsend-homelab-edition

# Build and run
docker-compose up -d

# View logs
docker-compose logs -f
```

### Option 2: Use Pre-built Binary

```bash
# Clone the repository
git clone https://gitgud.io/Nesquin/godsend-homelab-edition.git
cd godsend-homelab-edition

# Run with pre-built binary
docker-compose -f docker-compose.prebuilt.yml up -d
```

## Configuration

### Ports

| Port | Description |
|------|-------------|
| 8080 | HTTP API Server |

### Volumes

| Path | Description |
|------|-------------|
| `./data/Ready` | Converted games (GOD format) |
| `./data/Temp` | Temporary files during processing |

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `TZ` | `UTC` | Timezone |

## Usage

Once running, the server is accessible at `http://your-server-ip:8080`

### API Endpoints

- `GET /browse?platform=360` - Browse Xbox 360 games
- `GET /browse?platform=xbox` - Browse Original Xbox games
- `GET /browse?type=dlc` - Browse DLC
- `GET /trigger?game=GameName` - Start processing a game
- `GET /status?game=GameName` - Check processing status
- `GET /debug` - View ready games
- `GET /files/` - Access converted games

### Xbox Client Setup

Copy the files from `client-scripts/` to your Xbox's Aurora Scripts folder:
- `GODSend.ini`
- `main.lua`
- `MenuSystem.lua`
- `Icon/` folder

Edit `GODSend.ini` to point to your server's IP address.

## Resource Requirements

- **Minimum RAM**: 512MB
- **Recommended RAM**: 2-4GB (for processing large games)
- **Disk Space**: Varies based on games (plan for 50GB+ for temp files during processing)

## Troubleshooting

### Check if container is running
```bash
docker-compose ps
```

### View logs
```bash
docker-compose logs -f godsend
```

### Restart container
```bash
docker-compose restart
```

### Rebuild after updates
```bash
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

### Check health status
```bash
docker inspect --format='{{.State.Health.Status}}' godsend
```

## Updating

```bash
# Pull latest changes
git pull

# Rebuild and restart
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

## Stopping

```bash
docker-compose down
```

To also remove the data volumes:
```bash
docker-compose down -v
```
