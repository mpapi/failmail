package configure

import (
	"fmt"
	p "github.com/hut8labs/failmail/parse"
	"io"
	"io/ioutil"
	"reflect"
	"strings"
)

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
				settings[strings.ToLower(key.Text)] = value.Text
			}
		}
	}
	return settings
}

func bind(settings map[string]string, config interface{}) {
	configPtr := reflect.ValueOf(config)
	configStruct := configPtr.Elem()
	configType := configStruct.Type()

	for i := 0; i < configType.NumField(); i++ {
		fieldType := configType.Field(i)
		fieldValue := configStruct.Field(i)
		if value, ok := settings[strings.ToLower(fieldType.Name)]; ok {
			fieldValue.Set(reflect.ValueOf(value))
		}
	}
}
