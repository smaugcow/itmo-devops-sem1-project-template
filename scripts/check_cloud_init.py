#!/usr/bin/env python3
import json
import sys
from pathlib import Path

KNOWN_WARNING = (
    'Stdout, stderr changing to (| tee -a /var/log/cloud-init-output.log /dev/console, '
    '| tee -a /var/log/cloud-init-output.log /dev/console)'
)


def ready(status, exit_code):
    if exit_code not in (0, 2) or status.get('status') != 'done':
        return False
    for stage in (status, *(status.get(name, {}) for name in
                           ('init-local', 'init', 'modules-config', 'modules-final'))):
        if stage.get('errors'):
            return False
        for level, messages in stage.get('recoverable_errors', {}).items():
            if messages and (level != 'WARNING' or any(message != KNOWN_WARNING for message in messages)):
                return False
    return True


if __name__ == '__main__':
    status = json.loads(Path(sys.argv[1]).read_text())
    if not ready(status, int(sys.argv[2])):
        sys.exit('cloud-init is not ready; inspect its status before retrying')
    if int(sys.argv[2]) == 2:
        print('cloud-init completed with the known output routing warning', file=sys.stderr)
