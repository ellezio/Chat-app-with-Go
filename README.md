## TODOs
### Features
#### Multiple clients
- [ ] CLI
- [ ] Desktop App
- [X] Web Client

### Architecure
- [ ] Queue for incomming messages to process - RabbitMQ
- [ ] Caching - Redis | Valkey
- [ ] File server for attachments

### Telemetry
- [ ] Logs monitoring - ELK stack
- [ ] Metrics - ?

### CI/CD
- [ ] High availablity deployment - implementing blue-green mechanism
- [ ] Automation - Jenkins | GitHub Action - can be both just to learn
- [ ] Feature flags

### Other
- [ ] Configuration managment - some research needed on some aproches

## Ideas
1. Data store resiliance
When the connection to Store Provider cannot be established
use Fallback Store that is local (i.e. SQLite, memcache, other file/mem solution).
And when connection is established stream all the data to Store Provider.
This give as continuous availibility in case Store failure.
But there have to be reserved space for Fallback Store
to not consume all available resources needed for main app to run.
