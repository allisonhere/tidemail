{
  description = "TideMail - keyboard-first terminal email client";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      version =
        if self ? shortRev then
          "unstable-${self.shortRev}"
        else if self ? dirtyShortRev then
          "unstable-${self.dirtyShortRev}"
        else
          "dev";
    in
    {
      packages = forAllSystems (
        pkgs:
        let
          lib = pkgs.lib;
          # Tools TideMail shells out to at runtime. Appended (not prepended) to
          # PATH so the user's own choices win when present.
          runtimeTools =
            lib.optionals pkgs.stdenv.hostPlatform.isLinux [
              pkgs.libsecret # secret-tool: system keychain for passwords/tokens
              pkgs.xdg-utils # xdg-open: links and OAuth browser sign-in
              pkgs.libnotify # notify-send: new-mail notifications
            ];
        in
        rec {
          tidemail = (pkgs.buildGoModule.override { go = pkgs.go_1_26; }) {
            pname = "tidemail";
            inherit version;
            src = lib.cleanSource self;

            # Update after any go.mod/go.sum change: set to lib.fakeHash,
            # build, and copy the "got:" hash from the error.
            vendorHash = "sha256-Ku3EWZ70b950XbJuDTvOOXF32Upk0CkbmiezXWuipoU=";

            # Only the main binary; cmd/* are developer tools.
            subPackages = [ "." ];
            env.CGO_ENABLED = 0;
            ldflags = [
              "-s"
              "-w"
              "-X main.version=${version}"
            ];

            nativeBuildInputs = [
              pkgs.installShellFiles
              pkgs.makeWrapper
            ];

            # The test suite touches the keychain, terminals and the network;
            # CI runs it. Keep the Nix build hermetic and fast.
            doCheck = false;

            postInstall = ''
              installManPage man/tidemail.1
              install -Dm644 packaging/linux/tidemail.desktop \
                $out/share/applications/tidemail.desktop
              install -Dm644 images/tidemail-icon.png \
                $out/share/icons/hicolor/256x256/apps/tidemail.png
            ''
            + lib.optionalString (runtimeTools != [ ]) ''
              wrapProgram $out/bin/tidemail \
                --suffix PATH : ${lib.makeBinPath runtimeTools}
            '';

            meta = {
              description = "Keyboard-first terminal email client";
              homepage = "https://github.com/allisonhere/tidemail";
              license = lib.licenses.mit;
              mainProgram = "tidemail";
              platforms = systems;
            };
          };
          default = tidemail;
        }
      );

      apps = forAllSystems (pkgs: {
        default = {
          type = "app";
          program = "${self.packages.${pkgs.stdenv.hostPlatform.system}.tidemail}/bin/tidemail";
        };
      });

      overlays.default = final: _prev: {
        tidemail = self.packages.${final.stdenv.hostPlatform.system}.tidemail;
      };

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          inputsFrom = [ self.packages.${pkgs.stdenv.hostPlatform.system}.tidemail ];
          packages = [
            pkgs.go_1_26
            pkgs.gopls
            pkgs.golangci-lint
            pkgs.delve
          ];
          # Match CI: pure-Go SQLite, no cgo toolchain needed.
          CGO_ENABLED = 0;
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt);
    };
}
