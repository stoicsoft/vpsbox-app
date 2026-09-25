# CapRover on vpsbox

CapRover expects wildcard DNS. In vpsbox, use the `sslip.io` wildcard base:

1. `vpsbox up --name caprover`
2. Read `domain_base` from `vpsbox info caprover`
3. Configure CapRover to use `captain.<domain_base>` or `*.<domain_base>`

The local root hostname remains useful for SSH and the dashboard, but the wildcard-safe domain for app routing is the `sslip.io` base.
