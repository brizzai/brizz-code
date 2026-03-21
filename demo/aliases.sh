# Source this file to get demo aliases:  source demo/aliases.sh

DEMO_DIR="${0:a:h}"
ROOT_DIR="${DEMO_DIR:h}"

alias demo-setup="bash $DEMO_DIR/setup.sh"
alias demo-cleanup="bash $DEMO_DIR/cleanup.sh"
alias demo-brizz="BRIZZ_DEMO_PREFIX=/tmp/brizz-demo PATH=$DEMO_DIR:\$PATH $ROOT_DIR/build/brizz-code"

echo "Demo aliases loaded:"
echo "  demo-setup    — create demo repos and sessions"
echo "  demo-cleanup  — tear down demo environment"
echo "  demo-brizz    — launch brizz-code in demo mode (fake gh + filtered repos)"
