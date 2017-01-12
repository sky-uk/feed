#!/usr/bin/python3

import signal
import sys
import time

def signal_handler(sig, frame):
    if sig == signal.SIGQUIT:
        print('Received sigquit, doing graceful shutdown')
        time.sleep(0.5)
        sys.exit(0)
    if sig == signal.SIGHUP:
        print('Received sighup, this would normally trigger a reload')

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
signal.signal(signal.SIGQUIT, signal_handler)
signal.signal(signal.SIGHUP, signal_handler)
signal.pause
time.sleep(5)
print('Quit after 5 seconds of nada')
sys.exit(-1)
