package io.pagecrawl.relay

import io.pagecrawl.relay.NetworkPolicy.Decision
import io.pagecrawl.relay.NetworkPolicy.Network
import org.junit.Assert.assertEquals
import org.junit.Test

class NetworkPolicyTest {
    private val wifi = Network(hasInternet = true, isWifiOrEthernet = true, isMetered = false)
    private val mobile = Network(hasInternet = true, isWifiOrEthernet = false, isMetered = true)

    // A hotspot or a travel router on a SIM is Wi-Fi that still spends a data plan.
    private val meteredWifi = Network(hasInternet = true, isWifiOrEthernet = true, isMetered = true)

    @Test
    fun runsOnWifi() {
        assertEquals(Decision.RUN, NetworkPolicy.decide(wifi, allowMobileData = false))
    }

    @Test
    fun waitsForWifiOnMobileDataByDefault() {
        assertEquals(Decision.WAITING_FOR_WIFI, NetworkPolicy.decide(mobile, allowMobileData = false))
    }

    @Test
    fun treatsMeteredWifiAsMobileData() {
        assertEquals(Decision.WAITING_FOR_WIFI, NetworkPolicy.decide(meteredWifi, allowMobileData = false))
        assertEquals(Decision.RUN, NetworkPolicy.decide(meteredWifi, allowMobileData = true))
    }

    @Test
    fun runsOnMobileDataOnlyWhenAllowed() {
        assertEquals(Decision.RUN, NetworkPolicy.decide(mobile, allowMobileData = true))
    }

    @Test
    fun hasNothingToRunOnWithoutInternet() {
        assertEquals(Decision.NO_NETWORK, NetworkPolicy.decide(null, allowMobileData = true))
        assertEquals(
            Decision.NO_NETWORK,
            NetworkPolicy.decide(wifi.copy(hasInternet = false), allowMobileData = true),
        )
    }
}
