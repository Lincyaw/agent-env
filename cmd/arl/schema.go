package main

import (
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type commandSchema struct {
	Name            string          `json:"name"`
	Use             string          `json:"use"`
	Aliases         []string        `json:"aliases,omitempty"`
	Short           string          `json:"short,omitempty"`
	Long            string          `json:"long,omitempty"`
	Flags           []flagSchema    `json:"flags,omitempty"`
	PersistentFlags []flagSchema    `json:"persistentFlags,omitempty"`
	Commands        []commandSchema `json:"commands,omitempty"`
}

type flagSchema struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default,omitempty"`
	Usage       string `json:"usage,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Deprecated  string `json:"deprecated,omitempty"`
	NoOptDefVal string `json:"noOptDefVal,omitempty"`
}

func commandSchemaFor(cmd *cobra.Command) commandSchema {
	schema := commandSchema{
		Name:            cmd.Name(),
		Use:             cmd.UseLine(),
		Aliases:         append([]string(nil), cmd.Aliases...),
		Short:           cmd.Short,
		Long:            cmd.Long,
		Flags:           flagSchemas(cmd.LocalNonPersistentFlags()),
		PersistentFlags: flagSchemas(cmd.PersistentFlags()),
	}

	commands := cmd.Commands()
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name() < commands[j].Name()
	})
	for _, child := range commands {
		if child.Hidden {
			continue
		}
		schema.Commands = append(schema.Commands, commandSchemaFor(child))
	}
	return schema
}

func flagSchemas(flags *pflag.FlagSet) []flagSchema {
	if flags == nil {
		return nil
	}
	var out []flagSchema
	flags.VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		out = append(out, flagSchema{
			Name:        flag.Name,
			Shorthand:   flag.Shorthand,
			Type:        flag.Value.Type(),
			Default:     flag.DefValue,
			Usage:       flag.Usage,
			Required:    flagRequired(flag),
			Deprecated:  flag.Deprecated,
			NoOptDefVal: flag.NoOptDefVal,
		})
	})
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func flagRequired(flag *pflag.Flag) bool {
	for key, values := range flag.Annotations {
		if key == cobra.BashCompOneRequiredFlag {
			return true
		}
		for _, value := range values {
			if value == "true" && key == "cobra_annotation_required" {
				return true
			}
		}
	}
	return false
}
