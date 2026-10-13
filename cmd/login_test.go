package cmd

import "testing"

func TestSelectLoginCommand_None(t *testing.T) {
	command, err := selectLoginCommand(false, false, false, false, false)
	if err != nil {
		t.Fatalf("no flags: %v", err)
	}
	if command != loginCommandNone {
		t.Errorf("command: got %d, want none", command)
	}
}

func TestSelectLoginCommand_Single(t *testing.T) {
	cases := []struct {
		name                                   string
		bootstrap, list, remove, reset, create bool
		want                                   loginCommand
	}{
		{"bootstrap", true, false, false, false, false, loginCommandBootstrap},
		{"list", false, true, false, false, false, loginCommandList},
		{"remove", false, false, true, false, false, loginCommandRemove},
		{"reset", false, false, false, true, false, loginCommandReset},
		{"create", false, false, false, false, true, loginCommandCreate},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			command, err := selectLoginCommand(test.bootstrap, test.list, test.remove, test.reset, test.create)
			if err != nil {
				t.Fatalf("single flag: %v", err)
			}
			if command != test.want {
				t.Errorf("command: got %d, want %d", command, test.want)
			}
		})
	}
}

func TestSelectLoginCommand_MultipleDenies(t *testing.T) {
	if _, err := selectLoginCommand(true, false, false, false, true); err == nil {
		t.Error("bootstrap plus create: want error, got nil")
	}
	if _, err := selectLoginCommand(false, true, true, false, false); err == nil {
		t.Error("list plus remove: want error, got nil")
	}
}
