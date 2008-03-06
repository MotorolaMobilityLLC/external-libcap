#!/bin/bash
#
# Run through a series of tests to try out the various capability
# manipulations posible through exec.
#

try_capsh () {
    echo "TEST: ./capsh $*"
    ./capsh "$@"
    if [ $? -ne 0 ]; then
	echo FAILED
	return 1
    else
	echo PASSED
	return 0
    fi
}

fail_capsh () {
    echo -n "EXPECT FAILURE: "
    try_capsh "$@"
    if [ $? -eq 1 ]; then
	return 0
    else
	echo "Undesired result - aborting"
	echo "PROBLEM TEST: $*"
	exit 1
    fi
}

pass_capsh () {
    echo -n "EXPECT SUCCESS: "
    try_capsh "$@"
    if [ $? -eq 0 ]; then
	return 0
    else
	echo "Undesired result - aborting"
	echo "PROBLEM TEST: $*"
	exit 1
    fi
}

pass_capsh --print

# Make a local non-setuid-0 version of ping
cp /bin/ping . && chmod -s ./ping

# Give it the forced capability it needs
./setcap all=ep ./ping
if [ $? -ne 0 ]; then
    echo "Failed to set all capabilities on file"
    exit 1
fi
./setcap cap_net_raw=ep ./ping
if [ $? -ne 0 ]; then
    echo "Failed to set single capability on ping file"
    exit 1
fi

# Explore keep_caps support
pass_capsh --keep=0 --keep=1 --keep=0 --keep=1 --print

rm -f tcapsh
cp capsh tcapsh
chown root.root tcapsh
chmod u+s tcapsh
ls -l tcapsh

# leverage keep caps maintain capabilities accross a change of uid
# from setuid root to capable luser (as per wireshark/dumpcap 0.99.7)
pass_capsh --uid=500 -- -c "./tcapsh --keep=1 --caps=\"cap_net_raw,cap_net_admin=ip\" --uid=500 --caps=\"cap_net_raw,cap_net_admin=pie\" --print"

# This fails, on 2.6.24, but shouldn't
pass_capsh --uid=500 -- -c "./tcapsh --keep=1 --caps=\"cap_net_raw,cap_net_admin=ip\" --uid=500 --forkfor=10 --caps= --print --killit=9 --print"

rm -f tcapsh

# only continue with these if --secbits is supported
./capsh --secbits=0x2f > /dev/null 2>&1
if [ $? -ne 0 ]; then
    echo "unable to test securebits manipulation - assume not supported (PASS)"
    exit 0
fi

pass_capsh --secbits=42 --print
fail_capsh --secbits=32 --keep=1 --keep=0 --print
pass_capsh --secbits=10 --keep=0 --keep=1 --print
fail_capsh --secbits=47 -- -c "ping -c1 localhost"

# Suppress uid=0 privilege
fail_capsh --secbits=47 --print -- -c "/bin/ping -c1 localhost"

# suppress uid=0 privilege and test this ping
pass_capsh --secbits=0x2f --print -- -c "./ping -c1 localhost"

# observe that the bounding set can be used to suppress this forced capability
fail_capsh --drop=cap_net_raw,cap_chown --secbits=0x2f --print -- -c "./ping -c1 localhost"

# change the way the capability is obtained (make it inheritable)
./setcap cap_net_raw=ei ./ping

pass_capsh --secbits=47 --inh=cap_net_raw --drop=cap_net_raw \
    --uid=500 --print -- -c "./ping -c1 localhost"

rm -f ./ping
