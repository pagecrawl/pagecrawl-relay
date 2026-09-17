package io.pagecrawl.relay

/**
 * Whether the relay may run on the network the phone is on right now.
 *
 * By default only on Wi-Fi or a wired connection that is not metered. Checks through a relay
 * can move a real amount of data, and a person who installed this on their phone did not
 * agree to spend their data plan on it, so mobile data is off unless they turn it on. A
 * metered Wi-Fi network (another phone's hotspot, a travel router on a SIM) counts as mobile
 * data for the same reason, which is why this reads the metered flag rather than only
 * asking whether the connection is Wi-Fi.
 */
object NetworkPolicy {
    data class Network(
        val hasInternet: Boolean,
        val isWifiOrEthernet: Boolean,
        val isMetered: Boolean,
    )

    enum class Decision {
        /** Relay on this network. */
        RUN,

        /** No usable internet connection. */
        NO_NETWORK,

        /** Connected, but only over mobile data (or a metered network) and that is not allowed. */
        WAITING_FOR_WIFI,
    }

    fun decide(network: Network?, allowMobileData: Boolean): Decision {
        if (network == null || !network.hasInternet) {
            return Decision.NO_NETWORK
        }

        if (network.isWifiOrEthernet && !network.isMetered) {
            return Decision.RUN
        }

        return if (allowMobileData) Decision.RUN else Decision.WAITING_FOR_WIFI
    }
}
