package config

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/locales/en"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	en_translations "github.com/go-playground/validator/v10/translations/en"
	"github.com/hashicorp/go-multierror"

	"github.com/kubeshop/botkube/pkg/conversation"
	"github.com/kubeshop/botkube/pkg/execute/command"
	multierrx "github.com/kubeshop/botkube/pkg/multierror"
)

const (
	regexConstraintsIncludeTag  = "rs-include-regex"
	invalidBindingTag           = "invalid_binding"
	invalidChannelNameTag       = "invalid_channel_name"
	invalidChannelIDTag         = "invalid_channel_id"
	conflictingPluginRepoTag    = "conflicting_plugin_repo"
	conflictingPluginVersionTag = "conflicting_plugin_version"
	invalidPluginDefinitionTag  = "invalid_plugin_definition"
	invalidAliasCommandTag      = "invalid_alias_command"
	invalidPluginRBACTag        = "invalid_plugin_rbac"
	invalidActionRBACTag        = "invalid_action_tag"
	appTokenPrefix              = "xapp-"
	botTokenPrefix              = "xoxb-"
)

var (
	warnsOnlyTags = map[string]struct{}{
		regexConstraintsIncludeTag: {},
		invalidChannelNameTag:      {},
		invalidChannelIDTag:        {},
	}

	slackChannelNameRegex = regexp.MustCompile(`^[a-z0-9-_]{1,79}$`)

	discordChannelIDRegex      = regexp.MustCompile(`^[0-9]*$`)
	mattermostChannelNameRegex = regexp.MustCompile(`^[\s\S]{1,64}$`)

	slackDocsURL      = "https://api.slack.com/methods/conversations.rename#naming"
	discordDocsURL    = "https://support.discord.com/hc/en-us/articles/206346498-Where-can-I-find-my-User-Server-Message-ID-"
	mattermostDocsURL = "https://docs.mattermost.com/channels/channel-naming-conventions.html"
)

// ValidateResult holds the validation results.
type ValidateResult struct {
	Criticals *multierror.Error
	Warnings  *multierror.Error
}

// pluginProvider defines behavior for providing Plugins
type pluginProvider interface {
	GetPlugins() Plugins
}

// ValidateStruct validates a given struct based on the `validate` field tag.
func ValidateStruct(in any) (ValidateResult, error) {
	validate := validator.New()

	trans := ut.New(en.New()).GetFallback() // Currently we don't support other that en translations.
	if err := en_translations.RegisterDefaultTranslations(validate, trans); err != nil {
		return ValidateResult{}, err
	}

	if err := registerCustomTranslations(validate, trans); err != nil {
		return ValidateResult{}, err
	}
	if err := registerRegexConstraintsValidator(validate, trans); err != nil {
		return ValidateResult{}, err
	}
	if err := registerBindingsValidator(validate, trans); err != nil {
		return ValidateResult{}, err
	}

	if err := registerAliasValidator(validate, trans); err != nil {
		return ValidateResult{}, err
	}

	validate.RegisterStructValidation(slackStructTokenValidator, Slack{})
	validate.RegisterStructValidation(socketSlackValidator, SocketSlack{})
	validate.RegisterStructValidation(discordValidator, Discord{})
	validate.RegisterStructValidation(cloudSlackValidator, CloudSlack{})
	validate.RegisterStructValidation(mattermostValidator, Mattermost{})

	validate.RegisterStructValidation(sourceStructValidator, Sources{})
	validate.RegisterStructValidation(executorStructValidator, Executors{})

	err := validate.Struct(in)
	if err == nil {
		return ValidateResult{}, nil
	}

	errs, ok := err.(validator.ValidationErrors)
	if !ok {
		return ValidateResult{}, err
	}
	result := ValidateResult{
		Criticals: multierrx.New(),
		Warnings:  multierrx.New(),
	}

	for _, e := range errs {
		msg := fmt.Errorf("Key: '%s' %s", e.StructNamespace(), e.Translate(trans))

		if _, found := warnsOnlyTags[e.Tag()]; found {
			result.Warnings = multierrx.Append(result.Warnings, msg)
			continue
		}

		result.Criticals = multierrx.Append(result.Criticals, msg)
	}
	return result, nil
}

func registerCustomTranslations(validate *validator.Validate, trans ut.Translator) error {
	return registerTranslation(validate, trans, map[string]string{
		"invalid_slack_token": "{0} {1}",
		invalidChannelNameTag: "The channel name '{0}' seems to be invalid. See the documentation to learn more: {1}.",
	})
}

func registerRegexConstraintsValidator(validate *validator.Validate, trans ut.Translator) error {
	// NOTE: only have to register a non-pointer type for 'RegexConstraints', validator
	// internally dereferences it.
	validate.RegisterStructValidation(regexConstraintsStructValidator, RegexConstraints{})

	return registerTranslation(validate, trans, map[string]string{
		regexConstraintsIncludeTag: "{0} contains multiple constraints, but it does already include a regex pattern for all values",
	})
}

func registerBindingsValidator(validate *validator.Validate, trans ut.Translator) error {
	validate.RegisterStructValidation(botBindingsStructValidator, BotBindings{})
	validate.RegisterStructValidation(actionBindingsStructValidator, ActionBindings{})
	validate.RegisterStructValidation(sinkBindingsStructValidator, SinkBindings{})

	return registerTranslation(validate, trans, map[string]string{
		invalidBindingTag:           "'{0}' binding not defined in {1}",
		conflictingPluginRepoTag:    "{0}{1}",
		conflictingPluginVersionTag: "{0}{1}",
		invalidPluginDefinitionTag:  "{0}{1}",
		invalidPluginRBACTag:        "Binding is referencing plugins of same kind with different RBAC. '{0}' and '{1}' bindings must be identical when used together.",
		invalidActionRBACTag:        "Plugin {0} has 'ChannelName' RBAC policy. This is not supported for actions. See https://docs.botkube.io/configuration/action#rbac",
	})
}

func registerAliasValidator(validate *validator.Validate, trans ut.Translator) error {
	validate.RegisterStructValidation(aliasesStructValidator, Alias{})

	return registerTranslation(validate, trans, map[string]string{
		invalidAliasCommandTag: "Command prefix '{0}' not found in executors or builtin commands",
	})
}

func slackStructTokenValidator(sl validator.StructLevel) {
	slack, ok := sl.Current().Interface().(Slack)

	if !ok || !slack.Enabled {
		return
	}

	if slack.Token == "" {
		sl.ReportError(slack.Token, "Token", "Token", "required", "")
		return
	}

	if !strings.HasPrefix(slack.Token, botTokenPrefix) {
		msg := fmt.Sprintf("must have the %s prefix. Learn more at https://docs.botkube.io/installation/slack/#install-botkube-slack-app-to-your-slack-workspace", botTokenPrefix)
		sl.ReportError(slack.Token, "Token", "Token", "invalid_slack_token", msg)
	}
}

func socketSlackValidator(sl validator.StructLevel) {
	slack, ok := sl.Current().Interface().(SocketSlack)

	if !ok || !slack.Enabled {
		return
	}

	if slack.AppToken == "" {
		sl.ReportError(slack.AppToken, "AppToken", "AppToken", "required", "")
	}

	if slack.BotToken == "" {
		sl.ReportError(slack.BotToken, "BotToken", "BotToken", "required", "")
	}

	if !strings.HasPrefix(slack.BotToken, botTokenPrefix) {
		msg := fmt.Sprintf("must have the %s prefix. Learn more at https://docs.botkube.io/installation/socketslack/#obtain-bot-token", botTokenPrefix)
		sl.ReportError(slack.BotToken, "BotToken", "BotToken", "invalid_slack_token", msg)
	}

	if !strings.HasPrefix(slack.AppToken, appTokenPrefix) {
		msg := fmt.Sprintf("must have the %s prefix. Learn more at https://docs.botkube.io/installation/socketslack/#generate-and-obtain-app-level-token", appTokenPrefix)
		sl.ReportError(slack.AppToken, "AppToken", "AppToken", "invalid_slack_token", msg)
	}

	validateChannels(sl, slackChannelNameRegex, true, slack.Channels, "Name", slackDocsURL)
}

func cloudSlackValidator(sl validator.StructLevel) {
	slack, ok := sl.Current().Interface().(CloudSlack)

	if !ok || !slack.Enabled {
		return
	}

	validateChannels(sl, slackChannelNameRegex, true, slack.Channels, "Name", slackDocsURL)
}

func discordValidator(sl validator.StructLevel) {
	discord, ok := sl.Current().Interface().(Discord)

	if !ok || !discord.Enabled {
		return
	}

	validateChannels(sl, discordChannelIDRegex, true, discord.Channels, "ID", discordDocsURL)
}

func mattermostValidator(sl validator.StructLevel) {
	mattermost, ok := sl.Current().Interface().(Mattermost)

	if !ok || !mattermost.Enabled {
		return
	}

	validateChannels(sl, mattermostChannelNameRegex, false, mattermost.Channels, "Name", mattermostDocsURL)
}

func validateChannels[T Identifiable](sl validator.StructLevel, regex *regexp.Regexp, shouldNormalize bool, channels IdentifiableMap[T], fieldName, docsURL string) {
	if len(channels) == 0 {
		sl.ReportError(channels, "Channels", "Channels", "required", "")
	}

	for channelAlias, channel := range channels {
		if channel.Identifier() == "" {
			sl.ReportError(channel.Identifier(), fieldName, fieldName, "required", "")
			return
		}

		channelIdentifier := channel.Identifier()
		if shouldNormalize {
			channelIdentifier, _ = conversation.NormalizeChannelIdentifier(channel.Identifier())
		}
		valid := regex.MatchString(channelIdentifier)
		if !valid {
			fieldNameWithAliasProp := fmt.Sprintf("%s.%s", channelAlias, fieldName)
			sl.ReportError(fieldName, channel.Identifier(), fieldNameWithAliasProp, invalidChannelNameTag, docsURL)
		}
	}
}

func regexConstraintsStructValidator(sl validator.StructLevel) {
	rc, ok := sl.Current().Interface().(RegexConstraints)
	if !ok {
		return
	}

	if len(rc.Include) < 2 {
		return
	}

	foundAllValuesPattern := func() bool {
		for _, name := range rc.Include {
			if name == allValuesPattern {
				return true
			}
		}
		return false
	}

	if foundAllValuesPattern() {
		sl.ReportError(rc.Include, "Include", "Include", regexConstraintsIncludeTag, "")
	}
}

func sourceStructValidator(sl validator.StructLevel) {
	sources, ok := sl.Current().Interface().(Sources)
	if !ok {
		return
	}

	validatePlugins(sl, sources.Plugins)
}

func executorStructValidator(sl validator.StructLevel) {
	executor, ok := sl.Current().Interface().(Executors)
	if !ok {
		return
	}

	validatePlugins(sl, executor.Plugins)
}

func botBindingsStructValidator(sl validator.StructLevel) {
	bindings, ok := sl.Current().Interface().(BotBindings)
	if !ok {
		return
	}
	conf, ok := sl.Top().Interface().(Config)
	if !ok {
		return
	}
	validateSourceBindings(sl, conf.Sources, bindings.Sources)
	validateExecutorBindings(sl, conf.Executors, bindings.Executors)
}

func actionBindingsStructValidator(sl validator.StructLevel) {
	bindings, ok := sl.Current().Interface().(ActionBindings)
	if !ok {
		return
	}
	conf, ok := sl.Top().Interface().(Config)
	if !ok {
		return
	}
	validateSourceBindings(sl, conf.Sources, bindings.Sources)
	validateExecutorBindings(sl, conf.Executors, bindings.Executors)
	validateActionExecutors(sl, conf.Executors, bindings.Executors)
}

func aliasesStructValidator(sl validator.StructLevel) {
	alias, ok := sl.Current().Interface().(Alias)
	if !ok {
		return
	}
	conf, ok := sl.Top().Interface().(Config)
	if !ok {
		return
	}

	if alias.Command == "" {
		// validated on struct level, no need to report two errors
		return
	}

	cmdPrefix, _, _ := strings.Cut(alias.Command, " ")

	var prefixesToCheck []string
	// collect executors
	for _, exec := range conf.Executors {
		prefixesToCheck = append(prefixesToCheck, exec.CollectCommandPrefixes()...)
	}
	// collect builtin commands
	for _, verb := range command.AllVerbs() {
		prefixesToCheck = append(prefixesToCheck, string(verb))
	}

	for _, prefix := range prefixesToCheck {
		if prefix != cmdPrefix {
			continue
		}

		// command prefix is valid
		return
	}

	sl.ReportError(alias.Command, cmdPrefix, "Command", invalidAliasCommandTag, "")
}

func sinkBindingsStructValidator(sl validator.StructLevel) {
	bindings, ok := sl.Current().Interface().(SinkBindings)
	if !ok {
		return
	}
	conf, ok := sl.Top().Interface().(Config)
	if !ok {
		return
	}
	validateSourceBindings(sl, conf.Sources, bindings.Sources)
}

func validateSourceBindings(sl validator.StructLevel, sources map[string]Sources, bindings []string) {
	var enabledPluginsViaBindings []string
	for _, source := range bindings {
		sourceConf, ok := sources[source]
		if !ok {
			sl.ReportError(bindings, source, source, invalidBindingTag, "Config.Sources")
		}
		for pluginKey, plugin := range sourceConf.Plugins {
			if !plugin.Enabled {
				continue
			}

			enabledPluginsViaBindings = append(enabledPluginsViaBindings, pluginKey)
		}
	}

	validateBoundPlugins(sl, enabledPluginsViaBindings)
	validatePluginRBAC(sl, sources, bindings)
}

func validatePluginRBAC[P pluginProvider](sl validator.StructLevel, pluginConfigs map[string]P, bindings []string) {
	// 1. identify duplicates
	groups := make(map[string][]string)
	for _, b := range bindings {
		plugins := pluginConfigs[b]
		for pluginKey, plugin := range plugins.GetPlugins() {
			if !plugin.Enabled {
				continue
			}
			groups[pluginKey] = append(groups[pluginKey], b)
		}
	}

	// 2. compare RBAC of duplicates
	for plugin, occurrences := range groups {
		if len(occurrences) < 2 {
			continue
		}

		// take the head of occurrences
		p1 := occurrences[0]
		p1Cfg, ok := pluginConfigs[p1].GetPlugins()[plugin]
		if !ok {
			continue
		}

		firstRBAC := p1Cfg.Context.RBAC
		// compare the head with the tail
		for i := 1; i < len(occurrences); i++ {
			nextIdx := occurrences[i]
			nextCfg, ok := pluginConfigs[nextIdx].GetPlugins()[plugin]
			if !ok {
				continue
			}

			if !reflect.DeepEqual(firstRBAC, nextCfg.Context.RBAC) {
				sl.ReportError(bindings, p1, p1, invalidPluginRBACTag, nextIdx)
			}
		}
	}
}

func validateActionExecutors(sl validator.StructLevel, executors map[string]Executors, bindings []string) {
	for _, executor := range bindings {
		execConf := executors[executor]
		for pluginKey, plugin := range execConf.Plugins {
			if !plugin.Enabled {
				continue
			}
			if plugin.Context.RBAC == nil {
				continue
			}
			if plugin.Context.RBAC.Group.Type == ChannelNamePolicySubjectType {
				sl.ReportError(bindings, pluginKey, executor, invalidActionRBACTag, "")
			}
		}
	}
}

func validateExecutorBindings(sl validator.StructLevel, executors map[string]Executors, bindings []string) {
	var enabledPluginsViaBindings []string
	for _, executor := range bindings {
		execConf, ok := executors[executor]
		if !ok {
			sl.ReportError(bindings, executor, executor, invalidBindingTag, "Config.Executors")
		}

		for pluginKey, plugin := range execConf.Plugins {
			if !plugin.Enabled {
				continue
			}

			enabledPluginsViaBindings = append(enabledPluginsViaBindings, pluginKey)
		}
	}

	validateBoundPlugins(sl, enabledPluginsViaBindings)
	validatePluginRBAC(sl, executors, bindings)
}

func registerTranslation(validate *validator.Validate, translator ut.Translator, translation map[string]string) error {
	for tag, text := range translation {
		registerFn := func(ut ut.Translator) error {
			return ut.Add(tag, text, false)
		}

		err := validate.RegisterTranslation(tag, translator, registerFn, translateFunc)
		if err != nil {
			return err
		}
	}

	return nil
}

// copied from: https://github.com/go-playground/validator/blob/9e2ea4038020b5c7e3802a21cfa4e3afcfdcd276/translations/en/en.go#L1391-L1399
func translateFunc(ut ut.Translator, fe validator.FieldError) string {
	t, err := ut.T(fe.Tag(), fe.Field(), fe.Param())
	if err != nil {
		return fe.(error).Error()
	}

	return t
}
