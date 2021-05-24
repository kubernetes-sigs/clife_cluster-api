# -*- mode: Python -*-

# set defaults

envsubst_cmd = "./hack/tools/bin/envsubst"
kustomize_cmd = "./hack/tools/bin/kustomize"

settings = {
    "deploy_cert_manager": True,
    "preload_images_for_kind": True,
    "enable_providers": ["docker"],
    "kind_cluster_name": "kind",
}

# global settings
settings.update(read_json(
    "tilt-settings.json",
    default = {},
))

if settings.get("trigger_mode") == "manual":
    trigger_mode(TRIGGER_MODE_MANUAL)

allow_k8s_contexts(settings.get("allowed_contexts"))

default_registry(settings.get("default_registry"))

always_enable_providers = ["core"]
extra_args = settings.get("extra_args", {})

providers = {
    "core": {
        "context": ".",
        "image": "gcr.io/k8s-staging-cluster-api/cluster-api-controller",
        "live_reload_deps": [
            "main.go",
            "go.mod",
            "go.sum",
            "api",
            "cmd",
            "controllers",
            "errors",
            "third_party",
            "util",
            "exp",
            "feature",
        ],
    },
    "kubeadm-bootstrap": {
        "context": "bootstrap/kubeadm",
        "image": "gcr.io/k8s-staging-cluster-api/kubeadm-bootstrap-controller",
        "live_reload_deps": [
            "main.go",
            "api",
            "controllers",
            "internal",
        ],
    },
    "kubeadm-control-plane": {
        "context": "controlplane/kubeadm",
        "image": "gcr.io/k8s-staging-cluster-api/kubeadm-control-plane-controller",
        "live_reload_deps": [
            "main.go",
            "api",
            "controllers",
            "internal",
        ],
    },
    "docker": {
        "context": "test/infrastructure/docker",
        "image": "gcr.io/k8s-staging-cluster-api/capd-manager",
        "live_reload_deps": [
            "main.go",
            "go.mod",
            "go.sum",
            "api",
            "cloudinit",
            "controllers",
            "docker",
            "exp",
            "third_party",
        ],
        "additional_docker_helper_commands": """
RUN wget -qO- https://dl.k8s.io/v1.19.2/kubernetes-client-linux-amd64.tar.gz | tar xvz
RUN wget -qO- https://get.docker.com | sh
""",
        "additional_docker_build_commands": """
COPY --from=tilt-helper /usr/bin/docker /usr/bin/docker
COPY --from=tilt-helper /go/kubernetes/client/bin/kubectl /usr/bin/kubectl
""",
    },
}

# Reads a provider's tilt-provider.json file and merges it into the providers map.
# A list of dictionaries is also supported by enclosing it in brackets []
# An example file looks like this:
# {
#     "name": "aws",
#     "config": {
#         "image": "gcr.io/k8s-staging-cluster-api-aws/cluster-api-aws-controller",
#         "live_reload_deps": [
#             "main.go", "go.mod", "go.sum", "api", "cmd", "controllers", "pkg"
#         ]
#     }
# }


tilt_helper_dockerfile_header = """
# Tilt image
FROM golang:1.16.2 as tilt-helper
# Support live reloading with Tilt
RUN wget --output-document /restart.sh --quiet https://raw.githubusercontent.com/windmilleng/rerun-process-wrapper/master/restart.sh  && \
    wget --output-document /start.sh --quiet https://raw.githubusercontent.com/windmilleng/rerun-process-wrapper/master/start.sh && \
    chmod +x /start.sh && chmod +x /restart.sh
"""

tilt_dockerfile_header = """
FROM gcr.io/distroless/base:debug as tilt
WORKDIR /
COPY --from=tilt-helper /start.sh .
COPY --from=tilt-helper /restart.sh .
COPY manager .
"""

# Configures a provider by doing the following:
#
# 1. Enables a local_resource go build of the provider's manager binary
# 2. Configures a docker build for the provider, with live updating of the manager binary
# 3. Runs kustomize for the provider's config/default and applies it
def enable_provider(name):
    p = providers.get(name)

    context = p.get("context")
    go_main = p.get("go_main", "main.go")

    # Prefix each live reload dependency with context. For example, for if the context is
    # test/infra/docker and main.go is listed as a dep, the result is test/infra/docker/main.go. This adjustment is
    # needed so Tilt can watch the correct paths for changes.
    live_reload_deps = []
    for d in p.get("live_reload_deps", []):
        live_reload_deps.append(context + "/" + d)

    # Set up a local_resource build of the provider's manager binary. The provider is expected to have a main.go in
    # manager_build_path or the main.go must be provided via go_main option. The binary is written to .tiltbuild/manager.
    local_resource(
        name + "_manager",
        cmd = "cd " + context + ';mkdir -p .tiltbuild;CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags \'-extldflags "-static"\' -o .tiltbuild/manager ' + go_main,
        deps = live_reload_deps,
    )

    additional_docker_helper_commands = p.get("additional_docker_helper_commands", "")
    additional_docker_build_commands = p.get("additional_docker_build_commands", "")

    dockerfile_contents = "\n".join([
        tilt_helper_dockerfile_header,
        additional_docker_helper_commands,
        tilt_dockerfile_header,
        additional_docker_build_commands,
    ])

    # Set up an image build for the provider. The live update configuration syncs the output from the local_resource
    # build into the container.
    entrypoint = ["sh", "/start.sh", "/manager"]
    provider_args = extra_args.get(name)
    if provider_args:
        entrypoint.extend(provider_args)

    docker_build_with_restart(
        ref = p.get("image"),
        context = context + "/.tiltbuild/",
        dockerfile_contents = dockerfile_contents,
        target = "tilt",
        entrypoint = entrypoint,
        only = "manager",
        live_update = [
            sync(context + "/.tiltbuild/manager", "/manager"),
            run("sh /restart.sh"),
        ],
    )

    if p.get("kustomize_config", True):
        # Copy all the substitutions from the user's tilt-settings.json into the environment. Otherwise, the substitutions
        # are not available and their placeholders will be replaced with the empty string when we call kustomize +
        # envsubst below.
        substitutions = settings.get("kustomize_substitutions", {})
        os.environ.update(substitutions)

        # Apply the kustomized yaml for this provider
        yaml = str(kustomize_with_envsubst(context + "/config/default"))
        k8s_yaml(blob(yaml))

# Users may define their own Tilt customizations in tilt.d. This directory is excluded from git and these files will
# not be checked in to version control.
def include_user_tilt_files():
    user_tiltfiles = listdir("tilt.d")
    for f in user_tiltfiles:
        include(f)

# Enable core cluster-api plus everything listed in 'enable_providers' in tilt-settings.json
def enable_providers():
    local("make kustomize envsubst")

    user_enable_providers = settings.get("enable_providers", [])
    provider_repos = settings.get("provider_repos", [])

    for provider_repo in provider_repos:
        provider_tiltfile = provider_repo + 'Tiltfile'
        load(provider_tiltfile, '_provider_name')
        if _provider_name in user_enable_providers:
            load(provider_tiltfile, '_load_provider')
            _load_provider()

def kustomize_with_envsubst(path):
    return str(local("{} build {} | {}".format(kustomize_cmd, path, envsubst_cmd), quiet = True))

##############################
# Actual work happens here
##############################
include_user_tilt_files()

load("ext://restart_process", "docker_build_with_restart")
load("ext://cert_manager", "deploy_cert_manager")

if settings.get("deploy_cert_manager"):
    deploy_cert_manager(version = "v1.1.0")

enable_providers()
