## Setting Development Environment Locally

### Up All Services

```bash
# up = build image + start service
make -f Makefile.local up
````

### Up a Single Service
```bash
# up = build image + start service
make -f Makefile.local up service=l1_chain
````

### Down All Services

```bash
# down = destroy all containers with images kept; you can not down a single service
make -f Makefile.local down
````

### Start/Stop All Services

```shell
make -f Makefile.local start
make -f Makefile.local stop
```

### Start/Stop a Single Service
equal to `docker start/stop bitl1chain` (container name, not service name)
```shell
make -f Makefile.local start service=l1_chain
make -f Makefile.local stop service=l1_chain
```

### Restart All Services
```bash
make -f Makefile.local restart
```

### Restart a Single Service
```bash
make -f Makefile.local restart service=l1_chain
```

### List Containers/Services
```bash
make -f Makefile.local ps
```

### Rebuild All Service Images
```shell

make -f Makefile.local build

```

### Rebuild A Single Service Image
```shell

make -f Makefile.local build service=l1_chain

```

## Some Key Points

### Multiple docker compose files

ref: https://docs.docker.com/compose/extends/#example-use-case

```shell
docker-compose -f docker-compose.yml \
              -f docker-compose.override.yml up
```

This feature can be used to split / extend `docker-compose.yml`

### .env file

If you need to override some environment variables, use `.env`
`docker-compose` will load it automatically

It is ignored by `.gitignore` and `.dockerignore`.

```bash
cp template.local.env .env

vim .env
```
