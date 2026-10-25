# Executed as root through the owned VM's local Tart control channel.
umask 077
case "$expected_os:$(uname -s):$(uname -m)" in
  linux:Linux:aarch64|macos:Darwin:arm64) ;;
  *) echo 'Guest OS/architecture does not match the selected image' >&2; exit 1;;
esac
if [ "$expected_os" = macos ]; then
  home=$(dscl . -read "/Users/$user" NFSHomeDirectory | cut -d ' ' -f 2-)
  group=staff
else
  home=$(getent passwd "$user" | cut -d: -f6)
  group=$(id -gn "$user")
fi
test -n "$home" && test -d "$home"
test ! -L "$home/.ssh"
mkdir -p "$home/.ssh" "$home/.agent-vm/bootstrap"
chmod 700 "$home/.ssh" "$home/.agent-vm" "$home/.agent-vm/bootstrap"
test ! -L "$home/.ssh/authorized_keys"
touch "$home/.ssh/authorized_keys"
grep -qxF "$public_key" "$home/.ssh/authorized_keys" || printf '%s\n' "$public_key" >> "$home/.ssh/authorized_keys"
chmod 600 "$home/.ssh/authorized_keys"
chown -R "$user:$group" "$home/.ssh" "$home/.agent-vm"
# Retain this per-clone key across interrupted setup, never reuse the image key.
key="$home/.agent-vm/bootstrap/ssh_host_ed25519_key"
if [ ! -f "$key" ]; then
  rm -f "$key.pending" "$key.pending.pub"
  ssh-keygen -q -t ed25519 -N '' -f "$key.pending"
  mv "$key.pending.pub" "$key.pub"
  mv "$key.pending" "$key"
fi
cp "$key" /etc/ssh/ssh_host_ed25519_key
cp "$key.pub" /etc/ssh/ssh_host_ed25519_key.pub
chmod 600 /etc/ssh/ssh_host_ed25519_key
if ! grep -q '^# agent-vm managed SSH$' /etc/ssh/sshd_config; then
  {
    printf '%s\n' '# agent-vm managed SSH' 'PasswordAuthentication no' 'KbdInteractiveAuthentication no' 'PubkeyAuthentication yes'
    cat /etc/ssh/sshd_config
  } > /etc/ssh/sshd_config.agent-vm
  chmod 600 /etc/ssh/sshd_config.agent-vm
  mv /etc/ssh/sshd_config.agent-vm /etc/ssh/sshd_config
fi
if [ "$expected_os" = linux ]; then mkdir -p /run/sshd; chmod 755 /run/sshd; fi
/usr/sbin/sshd -t
if [ "$expected_os" = linux ]; then
  systemctl restart ssh
fi
cat /etc/ssh/ssh_host_ed25519_key.pub
