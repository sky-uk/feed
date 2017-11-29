#!/usr/bin/env python3.6

import signal
import sys
import time

def sigquit_handler(sig, frame):
    time.sleep(0.5)
    print('Received sigquit, doing graceful shutdown')
    sys.exit(0)

# Can't do anything in this handler - python libs are not thread safe, so not safe to call e.g. print.
def sighup_handler(sig, frame):
    pass

print('Running {}'.format(str(sys.argv)))

if sys.argv[1] == '-v':
    print('Asked for version')
    sys.exit(0)

if sys.argv[1] == '-t':
    print('Asked for config validation')
    sys.exit(0)

# The parent golang process blocks SIGQUIT in subprocesses, for some reason.
# So we unblock it manually - same as what nginx does.
signal.pthread_sigmask(signal.SIG_UNBLOCK, {signal.SIGQUIT, signal.SIGHUP})
signal.signal(signal.SIGQUIT, sigquit_handler)
signal.signal(signal.SIGHUP, sighup_handler)
signal.pause

startup_marker_file_name = str.join('/', sys.argv[2].split('/')[:-1]) + '/nginx-started'
with open(startup_marker_file_name, 'w') as f:
    f.write('started!')

time.sleep(5)
print('Quit after 5 seconds of nada')
sys.exit(-1)
