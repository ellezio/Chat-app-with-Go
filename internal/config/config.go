package config

type Configuration struct {
	Webapp     `json:"webapp"`
	ChatServer `json:"chatServer"`
	Redis      `json:"redis"`
	MongoDB    `json:"mongodb"`
	RabbitMQ   `json:"rabbitmq"`
}

type Webapp struct {
}

type ChatServer struct {
}

type Redis struct {
	Addr string `json:"addr"`
	Pass string `json:"pass"`
	DB   int    `json:"db"`
}

type MongoDB struct {
	ConnectionString string `json:"connectionString"`
}

type RabbitMQ struct {
	ConnectionString string `json:"connectionString"`
}
