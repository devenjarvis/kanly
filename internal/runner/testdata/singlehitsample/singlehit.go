package singlehitsample

type Config struct{ On bool }

func Enable(c *Config) {
	c.On = true
}
