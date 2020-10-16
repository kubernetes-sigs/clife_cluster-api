/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	completionBoilerPlate = `
# Copyright 2020 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
`

	bashCompletionFunc = `
__clusterctl_debug_out()
{
    local cmd="$1"
    __clusterctl_debug "${FUNCNAME[1]}: get completion by ${cmd}"
    eval "${cmd} 2>/dev/null"
}

__clusterctl_override_flag_list=(--kubeconfig --kubeconfig-context --namespace -n)
__clusterctl_override_flags()
{
    local ${__clusterctl_override_flag_list[*]##*-} two_word_of of var
    for w in "${words[@]}"; do
        if [ -n "${two_word_of}" ]; then
            # --kubeconfig-context flag of clusterctl corresponds to --context of kubectl.
            if [ "${two_word_of}" = "--kubeconfig-context" ]; then
                two_word_of="--context"
            fi
            eval "${two_word_of##*-}=\"${two_word_of}=\${w}\""
            two_word_of=
            continue
        fi
        for of in "${__clusterctl_override_flag_list[@]}"; do
            case "${w}" in
                ${of}=*)
                    eval "${of##*-}=\"${w}\""
                    ;;
                ${of})
                    two_word_of="${of}"
                    ;;
            esac
        done
    done
    for var in "${__clusterctl_override_flag_list[@]##*-}"; do
        if eval "test -n \"\$${var}\""; then
            eval "echo -n \${${var}}' '"
        fi
    done
}

# $1 is the name of resource (required)
# $2 is template string for kubectl get (optional)
__clusterctl_kubectl_parse_get()
{
    local template
    template="${2:-"{{ range .items  }}{{ .metadata.name }} {{ end }}"}"
    local kubectl_out
    if kubectl_out=$(__clusterctl_debug_out "kubectl get $(__clusterctl_override_flags) -o template --template=\"${template}\" \"$1\""); then
        COMPREPLY+=( $( compgen -W "${kubectl_out[*]}" -- "$cur" ) )
    fi
}

__clusterctl_kubectl_get_resource_namespace()
{
    __clusterctl_kubectl_parse_get "namespace"
}

__clusterctl_kubectl_get_resource_configmap()
{
    __clusterctl_kubectl_parse_get "configmap"
}

__clusterctl_kubectl_get_resource_cluster()
{
    __clusterctl_kubectl_parse_get "cluster"
}

# $1 has to be "contexts", "clusters" or "users"
__clusterctl_kubectl_parse_config()
{
    local template kubectl_out
    template="{{ range .$1  }}{{ .name }} {{ end }}"
    if kubectl_out=$(__clusterctl_debug_out "kubectl config $(__clusterctl_kubectl_override_flags) -o template --template=\"${template}\" view"); then
        COMPREPLY=( $( compgen -W "${kubectl_out[*]}" -- "$cur" ) )
    fi
}

__clusterctl_kubectl_config_get_contexts()
{
    __kubectl_parse_config "contexts"
}

__clusterctl_config_repositories()
{
    local clusterctl_out
    if clusterctl_out=$(__clusterctl_debug_out "clusterctl config repositories --output name --provider \"$1\""); then
        COMPREPLY=( $( compgen -W "${clusterctl_out[*]}" -- "$cur" ) )
    fi
}

__clusterctl_config_repositories_bootstrap()
{
    __clusterctl_config_repositories "bootstrap"
}

__clusterctl_config_repositories_control-plane()
{
    __clusterctl_config_repositories "control-plane"
}

__clusterctl_config_repositories_core()
{
    __clusterctl_config_repositories "core"
}

__clusterctl_config_repositories_infrastructure()
{
    __clusterctl_config_repositories "infrastructure"
}

__clusterctl_custom_func() {
    case "$last_command" in
        clusterctl_get_kubeconfig)
            __clusterctl_kubectl_get_resource_cluster
            return
            ;;
        *)
            ;;
    esac
}
`
)

var (
	completionLong = LongDesc(`
		Output shell completion code for the specified shell (bash or zsh).
		The shell code must be evaluated to provide interactive completion of
		clusterctl commands. This can be done by sourcing it from the
		.bash_profile.

		Note: this requires the bash-completion framework.

		To install it on macOS use Homebrew:
		    $ brew install bash-completion
		Once installed, bash_completion must be evaluated. This can be done by
		adding the following line to the .bash_profile
		    [[ -r "$(brew --prefix)/etc/profile.d/bash_completion.sh" ]] && . "$(brew --prefix)/etc/profile.d/bash_completion.sh"

		If bash-completion is not installed on Linux, please install the
		'bash-completion' package via your distribution's package manager.

		Note for zsh users: [1] zsh completions are only supported in versions of zsh >= 5.2`)

	completionExample = Examples(`
		# Install bash completion on macOS using Homebrew
		brew install bash-completion
		printf "\n# Bash completion support\nsource $(brew --prefix)/etc/bash_completion\n" >> $HOME/.bash_profile
		source $HOME/.bash_profile

		# Load the clusterctl completion code for bash into the current shell
		source <(clusterctl completion bash)

		# Write bash completion code to a file and source it from .bash_profile
		clusterctl completion bash > ~/.kube/clusterctl_completion.bash.inc
		printf "\n# clusterctl shell completion\nsource '$HOME/.kube/clusterctl_completion.bash.inc'\n" >> $HOME/.bash_profile
		source $HOME/.bash_profile

		# Load the clusterctl completion code for zsh[1] into the current shell
		source <(clusterctl completion zsh)`)

	completionCmd = &cobra.Command{
		Use:       "completion [bash|zsh]",
		Short:     "Output shell completion code for the specified shell (bash or zsh)",
		Long:      LongDesc(completionLong),
		Example:   completionExample,
		Args:      cobra.ExactArgs(1),
		RunE:      runCompletion,
		ValidArgs: GetSupportedShells(),
	}

	completionShells = map[string]func(cmd *cobra.Command) error{
		"bash": runCompletionBash,
		"zsh":  runCompletionZsh,
	}

	bashCompletionFlags = map[string]string{
		"namespace":                 "__clusterctl_kubectl_get_resource_namespace",
		"kubeconfig-context":        "__clusterctl_kubectl_config_get_contexts",
		"from-config-map":           "__clusterctl_kubectl_get_resource_configmap",
		"from-config-map-namespace": "__clusterctl_kubectl_get_resource_namespace",
		"target-namespace":          "__clusterctl_kubectl_get_resource_namespace",
		"watching-namespace":        "__clusterctl_kubectl_get_resource_namespace",
		"bootstrap":                 "__clusterctl_config_repositories_bootstrap",
		"core":                      "__clusterctl_config_repositories_core",
		"control-plane":             "__clusterctl_config_repositories_control-plane",
		"infrastructure":            "__clusterctl_config_repositories_infrastructure",
	}
)

// GetSupportedShells returns a list of supported shells
func GetSupportedShells() []string {
	shells := []string{}
	for s := range completionShells {
		shells = append(shells, s)
	}
	return shells
}

func init() {
	RootCmd.AddCommand(completionCmd)
}

func runCompletion(cmd *cobra.Command, args []string) error {
	run, found := completionShells[args[0]]
	if !found {
		return fmt.Errorf("unsupported shell type %q", args[0])
	}

	visitAllFlagSet(RootCmd, func(fs *pflag.FlagSet) {
		for name, completion := range bashCompletionFlags {
			if f := fs.Lookup(name); f != nil {
				if f.Annotations == nil {
					f.Annotations = map[string][]string{}
				}
				f.Annotations[cobra.BashCompCustom] = append(
					f.Annotations[cobra.BashCompCustom],
					completion,
				)
			}
		}
	})

	return run(cmd.Parent())
}

func visitAllFlagSet(x *cobra.Command, fn func(*pflag.FlagSet)) {
	var f func(x *cobra.Command)

	f = func(x *cobra.Command) {
		fn(x.Flags())
		fn(x.PersistentFlags())

		for _, y := range x.Commands() {
			f(y)
		}
	}

	f(x)
}

func runCompletionBash(cmd *cobra.Command) error {
	return cmd.GenBashCompletion(os.Stdout)
}

const (
	completionZshHead = "#compdef clusterctl\n"

	completionZshInitialization = `
__clusterctl_bash_source() {
	alias shopt=':'
	emulate -L sh
	setopt kshglob noshglob braceexpand
	source "$@"
}
__clusterctl_type() {
	# -t is not supported by zsh
	if [ "$1" == "-t" ]; then
		shift
		# fake Bash 4 to disable "complete -o nospace". Instead
		# "compopt +-o nospace" is used in the code to toggle trailing
		# spaces. We don't support that, but leave trailing spaces on
		# all the time
		if [ "$1" = "__clusterctl_compopt" ]; then
			echo builtin
			return 0
		fi
	fi
	type "$@"
}
__clusterctl_compgen() {
	local completions w
	completions=( $(compgen "$@") ) || return $?
	# filter by given word as prefix
	while [[ "$1" = -* && "$1" != -- ]]; do
		shift
		shift
	done
	if [[ "$1" == -- ]]; then
		shift
	fi
	for w in "${completions[@]}"; do
		if [[ "${w}" = "$1"* ]]; then
			echo "${w}"
		fi
	done
}
__clusterctl_compopt() {
	true # don't do anything. Not supported by bashcompinit in zsh
}
__clusterctl_ltrim_colon_completions()
{
	if [[ "$1" == *:* && "$COMP_WORDBREAKS" == *:* ]]; then
		# Remove colon-word prefix from COMPREPLY items
		local colon_word=${1%${1##*:}}
		local i=${#COMPREPLY[*]}
		while [[ $((--i)) -ge 0 ]]; do
			COMPREPLY[$i]=${COMPREPLY[$i]#"$colon_word"}
		done
	fi
}
__clusterctl_get_comp_words_by_ref() {
	cur="${COMP_WORDS[COMP_CWORD]}"
	prev="${COMP_WORDS[${COMP_CWORD}-1]}"
	words=("${COMP_WORDS[@]}")
	cword=("${COMP_CWORD[@]}")
}
__clusterctl_filedir() {
	# Don't need to do anything here.
	# Otherwise we will get trailing space without "compopt -o nospace"
	true
}
autoload -U +X bashcompinit && bashcompinit
# use word boundary patterns for BSD or GNU sed
LWORD='[[:<:]]'
RWORD='[[:>:]]'
if sed --help 2>&1 | grep -q 'GNU\|BusyBox'; then
	LWORD='\<'
	RWORD='\>'
fi
__clusterctl_convert_bash_to_zsh() {
	sed \
	-e 's/declare -F/whence -w/' \
	-e 's/_get_comp_words_by_ref "\$@"/_get_comp_words_by_ref "\$*"/' \
	-e 's/local \([a-zA-Z0-9_]*\)=/local \1; \1=/' \
	-e 's/flags+=("\(--.*\)=")/flags+=("\1"); two_word_flags+=("\1")/' \
	-e 's/must_have_one_flag+=("\(--.*\)=")/must_have_one_flag+=("\1")/' \
	-e "s/${LWORD}_filedir${RWORD}/__clusterctl_filedir/g" \
	-e "s/${LWORD}_get_comp_words_by_ref${RWORD}/__clusterctl_get_comp_words_by_ref/g" \
	-e "s/${LWORD}__ltrim_colon_completions${RWORD}/__clusterctl_ltrim_colon_completions/g" \
	-e "s/${LWORD}compgen${RWORD}/__clusterctl_compgen/g" \
	-e "s/${LWORD}compopt${RWORD}/__clusterctl_compopt/g" \
	-e "s/${LWORD}declare${RWORD}/builtin declare/g" \
	-e "s/\\\$(type${RWORD}/\$(__clusterctl_type/g" \
	<<'BASH_COMPLETION_EOF'
`

	completionZshTail = `
BASH_COMPLETION_EOF
}
__clusterctl_bash_source <(__clusterctl_convert_bash_to_zsh)
`
)

func runCompletionZsh(cmd *cobra.Command) error {
	fmt.Print(completionZshHead)
	fmt.Print(completionBoilerPlate)
	fmt.Print(completionZshInitialization)

	if err := cmd.GenBashCompletion(os.Stdout); err != nil {
		return err
	}

	fmt.Print(completionZshTail)

	return nil
}
