package configure

import (
	"flag"
	"fmt"
	p "github.com/hut8labs/failmail/parse"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"
)

var normalizeFlagPattern = regexp.MustCompile("([a-z])([A-Z])")

func ConfigParser() p.Parser {
	comment := p.Omit(p.Regexp(`\s*#.*\n`))
	blank := p.Omit(p.Regexp(`\s*\n`))
	line := p.Series(
		p.Label("key", p.Regexp(`\w+`)),
		p.Regexp(`\s*=\s*`),
		p.Label("value", p.Regexp(`[^\n]*`)),
		p.Literal("\n"))
	return p.ZeroOrMore(p.Any(comment, blank, line))
}

func ReadConfig(reader io.Reader, config interface{}) (err error) {
	bytes, err := ioutil.ReadAll(reader)
	if err != nil {
		return
	}

	parser := ConfigParser()
	rest, parsed := parser.Parse(string(bytes))
	if rest != "" {
		// TODO needs file/line/etc. info
		err = fmt.Errorf("failed to parse config file")
	}

	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()

	settings := walk(parsed)
	bind(settings, config)
	return
}

func walk(parsed *p.Node) map[string]string {
	settings := make(map[string]string, 0)
	for item := parsed.Next; item != nil; item = item.Next {
		if key, ok := item.Get("key"); ok && key.Text != "" {
			if value, ok := item.Get("value"); ok {
				settings[normalizeFlag(key.Text)] = value.Text
			}
		}
	}
	return settings
}

type field struct {
	Definition reflect.StructField
	Value      reflect.Value
}

func fields(structPointer interface{}) []*field {
	result := make([]*field, 0)

	pointerValue := reflect.ValueOf(structPointer)
	structValue := pointerValue.Elem()
	structType := structValue.Type()

	for i := 0; i < structType.NumField(); i++ {
		fieldType := structType.Field(i)
		fieldValue := structValue.Field(i)
		result = append(result, &field{fieldType, fieldValue})
	}
	return result
}

func bind(settings map[string]string, config interface{}) {
	for _, f := range fields(config) {
		if value, ok := settings[normalizeFlag(f.Definition.Name)]; ok {
			f.Value.Set(reflect.ValueOf(value))
		}
	}
}

func normalizeFlag(field string) string {
	field = strings.Replace(field, "_", "-", -1)
	return strings.ToLower(normalizeFlagPattern.ReplaceAllString(field, "$1-$2"))
}

func buildFlagSet(configWithDefaults interface{}, errorHandling flag.ErrorHandling) (*flag.FlagSet, map[string]reflect.Value, *string) {
	flagset := flag.NewFlagSet(os.Args[0], errorHandling)

	values := make(map[string]reflect.Value, 0)
	for _, f := range fields(configWithDefaults) {
		flagName := normalizeFlag(f.Definition.Name)
		flagHelp := string(f.Definition.Tag.Get("help"))
		values[flagName] = f.Value

		switch {
		case reflect.TypeOf("").AssignableTo(f.Definition.Type):
			flagset.String(flagName, f.Value.Interface().(string), flagHelp)
		case reflect.TypeOf(true).AssignableTo(f.Definition.Type):
			flagset.Bool(flagName, f.Value.Interface().(bool), flagHelp)
		case reflect.TypeOf(0.0).AssignableTo(f.Definition.Type):
			flagset.Float64(flagName, f.Value.Interface().(float64), flagHelp)
		case reflect.TypeOf(0).AssignableTo(f.Definition.Type):
			flagset.Int(flagName, f.Value.Interface().(int), flagHelp)
		case reflect.TypeOf(time.Duration(0)).AssignableTo(f.Definition.Type):
			flagset.Duration(flagName, f.Value.Interface().(time.Duration), flagHelp)
		}
	}

	configFile := flagset.String("config", "", "path to a config file")

	return flagset, values, configFile
}

func Parse(configWithDefaults interface{}) error {
	flagset, _, configFile := buildFlagSet(configWithDefaults, flag.ContinueOnError)
	flagset.Usage = func() {}

	err := flagset.Parse(os.Args[1:])

	if *configFile != "" {
		file, err := os.Open(*configFile)
		if err != nil {
			return err
		}

		err = ReadConfig(file, configWithDefaults)
		if err != nil {
			return err
		}
	}

	flagset2, fieldValues, _ := buildFlagSet(configWithDefaults, flag.ExitOnError)

	err = flagset2.Parse(os.Args[1:])
	if err != nil {
		return err
	}
	flagset2.VisitAll(func(f *flag.Flag) {
		if fieldValue, ok := fieldValues[f.Name]; ok {
			fieldValue.Set(reflect.ValueOf(f.Value.(flag.Getter).Get()))
		}
	})

	return nil
}
