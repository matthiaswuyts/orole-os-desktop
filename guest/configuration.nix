# The bot computer. Agents see an ordinary Linux (nix-ld + envfs give FHS paths); Orole writes this file,
# agents never run nixos-rebuild (direct kernel boot, system.switch disabled).
{ config, lib, pkgs, modulesPath, orole-guest, ... }:

let
  # The bot's account is uid 1000. Its name, home folder and the host name follow the bot's name, which the host
  # passes at each boot (fw_cfg opt/orole/user), so nothing below may name it: units use the uid and this stable
  # home, which /home/<name> is bind-mounted from.
  stableHome = "/home/.orole-user";

  # Boot-time identity and keyboard layout from the host's fw_cfg (opt/orole/user, opt/orole/keymap; neither is
  # secret). "reset" runs before userborn and gives uid 1000 its image name back, so userborn always finds the
  # account it manages; "apply" runs after it, renames the account (name fields of passwd, shadow, group, gshadow),
  # binds /home/<name> to the stable home, sets the host name and writes /run/orole/session.env for cage and wayvnc.
  identity = pkgs.writeShellScript "orole-identity" ''
    set -u
    export PATH=${lib.makeBinPath (with pkgs; [ coreutils gnugrep gawk util-linux ])}
    fw=/sys/firmware/qemu_fw_cfg/by_name/opt/orole

    fwvalue() {
      [ -r "$fw/$1/raw" ] || return 0
      head -c 64 "$fw/$1/raw" | tr -d '\000\n'
    }

    # edit FILE AWK-ARGS...: rewrite FILE through awk, keeping its mode and owner. userborn bind-mounts passwd, group
    # and shadow read-only onto themselves; such a file is unmounted for the edit and mounted read-only again.
    edit() {
      f=$1
      shift
      [ -f "$f" ] || return 0
      bound=
      if awk -v f="$f" '$5 == f { found = 1 } END { exit !found }' /proc/self/mountinfo; then
        umount "$f" && bound=1
      fi
      awk "$@" "$f" > "$f.orole" && chmod --reference="$f" "$f.orole" && chown --reference="$f" "$f.orole" && mv "$f.orole" "$f"
      rm -f "$f.orole"
      if [ -n "$bound" ]; then
        mount --bind "$f" "$f" && mount -o remount,bind,ro "$f"
      fi
    }

    swap='function swap(list, n, i, a, out) { n = split(list, a, ","); out = ""; for (i = 1; i <= n; i++) { if (a[i] == old) a[i] = new; out = out (i > 1 ? "," : "") a[i] } return out }'

    # rename NAME HOME: uid 1000 becomes NAME with home HOME.
    rename() {
      old=$(awk -F: '$3 == 1000 { print $1; exit }' /etc/passwd)
      [ -n "$old" ] || return 0
      edit /etc/passwd -v old="$old" -v new="$1" -v home="$2" 'BEGIN { FS = OFS = ":" } $3 == 1000 { $1 = new; $6 = home } { print }'
      [ "$old" != "$1" ] || return 0
      edit /etc/shadow -v old="$old" -v new="$1" 'BEGIN { FS = OFS = ":" } $1 == old { $1 = new } { print }'
      edit /etc/group -v old="$old" -v new="$1" "$swap"' BEGIN { FS = OFS = ":" } { $4 = swap($4); print }'
      edit /etc/gshadow -v old="$old" -v new="$1" "$swap"' BEGIN { FS = OFS = ":" } { $3 = swap($3); $4 = swap($4); print }'
    }

    case "$1" in
    reset)
      if [ -f /etc/passwd ]; then rename bot ${stableHome}; fi
      ;;
    apply)
      name=$(fwvalue user)
      echo "$name" | grep -Eqx '[a-z][a-z0-9-]{0,23}' || name=bot
      case "$name" in *-) name=bot ;; esac
      # The host never sends a system account's name; this is the guest's own check.
      if awk -F: -v n="$name" '$1 == n && $3 != 1000 { found = 1 } END { exit !found }' /etc/passwd /etc/group; then
        name=bot
      fi
      home=/home/$name
      install -d -m 0700 -o 1000 -g 100 ${stableHome}
      rename "$name" "$home"
      # Earlier names left empty mount points behind; the bot cannot write /home, so only those are empty.
      for d in /home/*; do
        [ "$d" = "$home" ] || rmdir "$d" 2>/dev/null || true
      done
      if ! mountpoint -q "$home"; then
        install -d -m 0755 -o 0 -g 0 "$home"
        mount --bind ${stableHome} "$home"
      fi
      echo "$name-computer" > /proc/sys/kernel/hostname

      keymap=$(fwvalue keymap)
      echo "$keymap" | grep -Eqx '[a-z0-9_()+-]{1,40}' || keymap=us
      layout=$(echo "$keymap" | awk -F'(' '{ print $1 }')
      variant=$(echo "$keymap" | awk -F'[()]' '{ print $2 }')
      [ -n "$layout" ] || layout=us
      wayvnc=$layout
      [ -z "$variant" ] || wayvnc="$layout-$variant"
      mkdir -p /run/orole
      printf 'XKB_DEFAULT_LAYOUT=%s\nXKB_DEFAULT_VARIANT=%s\nWAYVNC_KEYBOARD=%s\n' "$layout" "$variant" "$wayvnc" > /run/orole/session.env.new
      chmod 0644 /run/orole/session.env.new
      mv /run/orole/session.env.new /run/orole/session.env
      echo "orole-identity: account $name, home $home, host $name-computer, keyboard $keymap"
      ;;
    esac
  '';

  # Chromium in the kiosk compositor. CDP listens on guest loopback only (orole-guest relays it over the
  # channel); the profile lives on the bot's disk so logins survive restarts. Restarted if it exits.
  browser = pkgs.writeShellScript "orole-browser" ''
    mkdir -p ${stableHome}/.config/orole-chromium
    while true; do
      ${pkgs.chromium}/bin/chromium \
        --ozone-platform=wayland \
        --user-data-dir=${stableHome}/.config/orole-chromium \
        --remote-debugging-address=127.0.0.1 \
        --remote-debugging-port=9222 \
        --no-first-run --no-default-browser-check \
        --disable-gpu \
        --start-maximized \
        about:blank
      sleep 1
    done
  '';
in
{
  imports = [
    "${modulesPath}/profiles/minimal.nix"
    "${modulesPath}/profiles/qemu-guest.nix"
  ];

  system.stateVersion = "26.05";

  # Direct kernel boot: no bootloader, systemd initrd, no switch-to-configuration (perl-free activation).
  boot.loader.grub.enable = false;
  boot.initrd.systemd.enable = true;
  boot.initrd.availableKernelModules = [ "virtio_pci" "virtio_blk" "virtio_net" "virtio_console" "virtio_rng" "ext4" ];
  # The channel token arrives through QEMU fw_cfg, which the kernel only exposes in sysfs with qemu_fw_cfg
  # loaded (a module in the NixOS kernel). Loading it in the initrd makes it present before any stage-2
  # service, orole-guest included.
  boot.initrd.kernelModules = [ "qemu_fw_cfg" ];
  boot.kernelModules = [ "qemu_fw_cfg" ];
  boot.kernelParams = [ "console=ttyS0" "quiet" ];
  system.switch.enable = false;
  services.userborn.enable = true;
  documentation.enable = false;

  fileSystems."/" = { device = "/dev/disk/by-label/nixos"; fsType = "ext4"; autoResize = true; };

  networking.hostName = "orole-computer";
  networking.useNetworkd = true;
  systemd.network.networks."10-uplink" = { matchConfig.Name = "en* eth*"; networkConfig.DHCP = "ipv4"; };
  networking.firewall.enable = false;
  # QEMU user networking maps 10.0.2.2 (and fec0::2 over IPv6) to the host's loopback, where Orole and other
  # local services listen. Nothing in the guest needs it: orole-guest talks over virtio-serial, DNS is
  # 10.0.2.3. Only DHCP renewals (systemd-networkd, port 68, which the bot cannot bind) go to 10.0.2.2.
  # Root in the guest could undo this, and the bot has no sudo. The internet and the home LAN stay
  # reachable: a documented, accepted risk (spec, "Network").
  # Chromium's DevTools port (9222) and wayvnc (5900) listen on guest loopback for orole-guest, which runs as
  # root. The bot user (uid 1000: its commands, and Chromium's own pages) must not reach them, or a command could
  # drive the browser or watch the screen past the takeover lock and the per-site approvals. Listening is not
  # affected (no packet leaves for a bind).
  networking.nftables = {
    enable = true;
    tables.orole_host = {
      family = "inet";
      content = ''
        chain output {
          type filter hook output priority 0; policy accept;
          ip daddr 10.0.2.2 udp sport 68 udp dport 67 accept
          ip daddr 10.0.2.2 reject
          ip6 daddr fec0::2 reject
          meta skuid 1000 ip daddr 127.0.0.0/8 tcp dport { 5900, 9222 } reject with tcp reset
          meta skuid 1000 ip6 daddr ::1 tcp dport { 5900, 9222 } reject with tcp reset
        }
      '';
    };
  };

  users.mutableUsers = false;
  # No login path by design: root and bot have locked passwords, no sshd, no sudo. Agents reach the VM only
  # through orole-guest; the debug image (debug.nix) adds root autologin on the serial console.
  users.allowNoPasswordLogin = true;
  users.users.root.hashedPassword = "!";
  # The image names the account "bot"; orole-identity renames it to the bot's name at each boot and creates the home.
  users.users.bot = { isNormalUser = true; uid = 1000; home = stableHome; createHome = false; hashedPassword = "!"; };
  security.sudo.enable = false;

  # Only root (orole-guest) may open the host channel; the kernel already makes fw_cfg files 0400 root, the
  # tmpfiles line keeps the token that way if that ever changes.
  services.udev.extraRules = ''
    SUBSYSTEM=="virtio-ports", ATTR{name}=="org.orole.guest.0", OWNER="root", GROUP="root", MODE="0600"
  '';
  systemd.tmpfiles.rules = [ "z /sys/firmware/qemu_fw_cfg/by_name/opt/orole/token/raw 0400 root root - -" ];

  # Desktop: cage on wlroots' headless backend (no GPU device), software rendering; wayvnc exports it on
  # guest loopback for orole-guest's screen relay.
  services.cage = {
    enable = true;
    user = "1000"; # the account's name changes with the bot's (orole-identity); systemd takes a uid
    program = browser;
    environment = {
      WLR_BACKENDS = "headless";
      WLR_RENDERER = "pixman";
      WLR_LIBINPUT_NO_DEVICES = "1";
      WLR_HEADLESS_OUTPUTS = "1";
    };
  };
  # Restart forever: the default start limit (5 starts in 10 s) would leave a crashing service down.
  systemd.services."cage-tty1" = {
    wants = [ "orole-identity.service" ];
    after = [ "orole-identity.service" ];
    unitConfig.StartLimitIntervalSec = 0;
    # XKB_DEFAULT_LAYOUT/VARIANT: the host's keyboard layout (orole-identity).
    serviceConfig = { Restart = "always"; RestartSec = 2; EnvironmentFile = "-/run/orole/session.env"; };
  };
  systemd.services.orole-identity-reset = {
    description = "Give the bot account its image name back before userborn";
    unitConfig.DefaultDependencies = false;
    wantedBy = [ "sysinit.target" ];
    before = [ "userborn.service" "sysinit.target" ];
    after = [ "systemd-remount-fs.service" ];
    serviceConfig = { Type = "oneshot"; RemainAfterExit = true; ExecStart = "${identity} reset"; };
  };
  systemd.services.orole-identity = {
    description = "Name the bot account, home and host after the bot; set the keyboard layout";
    wantedBy = [ "multi-user.target" ];
    after = [ "userborn.service" "systemd-tmpfiles-setup.service" "systemd-modules-load.service" "local-fs.target" ];
    before = [ "systemd-user-sessions.service" ];
    serviceConfig = { Type = "oneshot"; RemainAfterExit = true; ExecStart = "${identity} apply"; };
  };
  systemd.services.wayvnc = {
    description = "VNC server for the bot computer's screen (guest loopback only)";
    after = [ "cage-tty1.service" "orole-identity.service" ];
    wants = [ "orole-identity.service" ];
    wantedBy = [ "graphical.target" ];
    unitConfig.StartLimitIntervalSec = 0;
    serviceConfig = {
      User = "1000";
      Environment = [ "XDG_RUNTIME_DIR=/run/user/1000" "WAYLAND_DISPLAY=wayland-0" ];
      EnvironmentFile = "-/run/orole/session.env";
      # --render-cursor draws the pointer into the picture, so the user sees it while in control; --keyboard maps
      # the viewer's keys with the host's layout (WAYVNC_KEYBOARD is <layout>[-<variant>], from orole-identity).
      ExecStart = "${pkgs.wayvnc}/bin/wayvnc --render-cursor --keyboard=\${WAYVNC_KEYBOARD} 127.0.0.1 5900";
      Restart = "always";
      RestartSec = 2;
    };
  };
  systemd.defaultUnit = "graphical.target";

  # Runs as root: it powers the VM off and owns the channel. It runs commands and terminals as bot.
  systemd.services.orole-guest = {
    description = "Orole guest agent (virtio-serial org.orole.guest.0)";
    # After orole-identity: it looks the account up by uid and takes its name and home from passwd.
    after = [ "systemd-modules-load.service" "systemd-udevd.service" "orole-identity.service" ];
    wants = [ "orole-identity.service" ];
    wantedBy = [ "multi-user.target" ];
    unitConfig.StartLimitIntervalSec = 0;
    serviceConfig = { ExecStart = lib.getExe orole-guest; Restart = "always"; RestartSec = 1; };
  };

  # Fonts for Chromium (without fontconfig and a font set, pages render as boxes).
  fonts.fontconfig.enable = true;
  fonts.packages = with pkgs; [ dejavu_fonts noto-fonts ];

  environment.systemPackages = with pkgs; [
    python3 uv nodejs git curl jq ripgrep chromium
    coreutils findutils gnugrep gnused gawk which file unzip zip gnutar xz
  ];
  programs.nix-ld = {
    enable = true;
    libraries = with pkgs; [ stdenv.cc.cc.lib zlib openssl glib nss nspr libgcc ];
  };
  services.envfs.enable = true;
  nix.settings.experimental-features = [ "nix-command" "flakes" ];
  services.journald.settings.Journal.SystemMaxUse = "64M";
}
