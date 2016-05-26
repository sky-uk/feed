#!/usr/bin/python3

import signal
import sys
import time

def signal_handler(signal, frame):
    print('Received sigquit, doing graceful shutdown')
    time.sleep(0.5)
    sys.exit(0)

print('Running {}'.format(str(sys.argv)))
# The parent golang process blocks SIGQUIT in subprocesses, for some reason.
# So we unblock it manually - same as what nginx does.
signal.pthread_sigmask(signal.SIG_UNBLOCK, {signal.SIGQUIT})
signal.signal(signal.SIGQUIT, signal_handler)
signal.pause
time.sleep(5)
print('Should have handled SIGQUIT')
sys.exit(-1)
