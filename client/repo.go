package client

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Repo struct {
	Server   string `yaml:"server"`
	RepoId   int32  `yaml:"repo"`
	ChangeId int64  `yaml:"change"`
}

func (r *Repo) Load(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("open repo file: %w", err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(r); err != nil {
		return fmt.Errorf("decode repo file: %w", err)
	}

	return nil
}

func (r *Repo) Save(file string) error {
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("create repo file: %w", err)
	}
	defer f.Close()

	_, _ = fmt.Fprint(f, "---\n")

	encoder := yaml.NewEncoder(f)
	encoder.SetIndent(4)
	if err := encoder.Encode(r); err != nil {
		return fmt.Errorf("encode repo file: %w", err)
	}

	return nil
}