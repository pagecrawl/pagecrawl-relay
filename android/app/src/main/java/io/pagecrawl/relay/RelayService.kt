package io.pagecrawl.relay

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.wifi.WifiManager
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import io.pagecrawl.relay.NetworkPolicy.Decision
import io.pagecrawl.relay.go.mobile.Relay
import java.util.concurrent.Executors
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** What the screen shows about the service. */
data class ServiceState(
    val running: Boolean = false,
    val decision: Decision? = null,
    val status: RelayStatus = RelayStatus(),
)

/** The service's state, for the screen to observe while both live in the same process. */
object RelayMonitor {
    private val mutable = MutableStateFlow(ServiceState())
    val state: StateFlow<ServiceState> = mutable.asStateFlow()

    internal fun update(state: ServiceState) {
        mutable.value = state
    }
}

/**
 * Runs the relay while the person has it switched on.
 *
 * A foreground service, because relaying is long-running work the person asked for and must
 * be able to see: Android shows its notification the whole time it runs, and that
 * notification is the honest answer to "is my phone relaying right now".
 *
 * The Go relay (package mobile, over package relay) does the actual work. This service only
 * decides WHEN it runs: when the key is set, the relay is switched on, and the network the
 * phone is on is one the person allowed (see [NetworkPolicy]). When the network changes it
 * starts or stops the relay rather than letting it retry against a connection it may not
 * use.
 *
 * While relaying it holds a partial wake lock and a Wi-Fi lock. Without them Android lets
 * the processor and the Wi-Fi radio sleep, the gateway's keep-alive goes unanswered, and
 * checks fail with the phone looking connected. That costs battery, which is why the screen
 * recommends running it on a charger, and why the locks are released the moment the relay
 * stops.
 */
class RelayService : Service() {
    // Relay.stop() waits for the relay to wind down, so starts and stops run one at a time
    // off the main thread and can never overlap.
    private val worker = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    private lateinit var connectivity: ConnectivityManager
    private lateinit var settings: RelaySettings
    private var relay: Relay? = null
    private var decision: Decision? = null
    private var capabilities: NetworkCapabilities? = null
    private var wakeLock: PowerManager.WakeLock? = null
    private var wifiLock: WifiManager.WifiLock? = null

    private val networkCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
            main.post {
                capabilities = caps
                reevaluate()
            }
        }

        override fun onLost(network: Network) {
            main.post {
                capabilities = null
                reevaluate()
            }
        }
    }

    private val statusTicker = object : Runnable {
        override fun run() {
            publish()
            main.postDelayed(this, STATUS_INTERVAL_MS)
        }
    }

    override fun onCreate() {
        super.onCreate()
        connectivity = getSystemService(ConnectivityManager::class.java)
        settings = RelaySettings(this)
        createChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // Android requires the notification within seconds of the service starting, so it
        // goes up before anything that could take time.
        ServiceCompat.startForeground(this, NOTIFICATION_ID, notification(), foregroundType())

        if (intent?.action == ACTION_STOP) {
            settings.enabled = false
            stopSelf()
            return START_NOT_STICKY
        }

        val key = KeyVault(this).read()
        if (key == null || !settings.enabled) {
            stopSelf()
            return START_NOT_STICKY
        }

        if (relay == null) {
            relay = Relay(key, platform(), BuildConfig.VERSION_NAME)
            connectivity.registerDefaultNetworkCallback(networkCallback)
            main.post(statusTicker)
        } else if (intent?.action == ACTION_KEY_CHANGED) {
            val current = relay
            worker.execute { runCatching { current?.setToken(key) } }
        }

        reevaluate()

        // Restarted by Android after being killed for memory, it picks up where it left off.
        return START_STICKY
    }

    override fun onDestroy() {
        main.removeCallbacks(statusTicker)
        runCatching { connectivity.unregisterNetworkCallback(networkCallback) }

        val stopping = relay
        relay = null
        worker.execute {
            runCatching { stopping?.stop() }
        }
        worker.shutdown()
        releaseLocks()

        RelayMonitor.update(ServiceState())
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    /** Starts or stops the relay for the network the phone is on now. */
    private fun reevaluate() {
        val current = relay ?: return
        val next = NetworkPolicy.decide(network(capabilities), settings.allowMobileData)

        if (next != decision) {
            decision = next
            if (next == Decision.RUN) {
                acquireLocks()
                worker.execute { current.start() }
            } else {
                worker.execute { current.stop() }
                releaseLocks()
            }
        }

        publish()
    }

    private fun publish() {
        val status = RelayStatus.fromJson(relay?.statusJSON())
        RelayMonitor.update(ServiceState(running = relay != null, decision = decision, status = status))
        getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification(status))
    }

    private fun notification(status: RelayStatus = RelayStatus()): Notification {
        val open = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE,
        )
        val stop = PendingIntent.getService(
            this,
            1,
            Intent(this, RelayService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE,
        )

        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(headline(decision, status))
            .setContentText(detail(decision, status))
            .setContentIntent(open)
            .addAction(0, "Turn off", stop)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .build()
    }

    private fun createChannel() {
        val channel = NotificationChannel(CHANNEL_ID, "Relay status", NotificationManager.IMPORTANCE_LOW)
        channel.description = "Shown while this phone relays PageCrawl checks."
        getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
    }

    private fun acquireLocks() {
        if (wakeLock == null) {
            wakeLock = getSystemService(PowerManager::class.java)
                .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "PageCrawlRelay:relay")
                .apply { setReferenceCounted(false); acquire() }
        }
        if (wifiLock == null) {
            @Suppress("DEPRECATION")
            wifiLock = applicationContext.getSystemService(WifiManager::class.java)
                .createWifiLock(WifiManager.WIFI_MODE_FULL_HIGH_PERF, "PageCrawlRelay:relay")
                .apply { setReferenceCounted(false); acquire() }
        }
    }

    private fun releaseLocks() {
        wakeLock?.takeIf { it.isHeld }?.release()
        wakeLock = null
        wifiLock?.takeIf { it.isHeld }?.release()
        wifiLock = null
    }

    private fun foregroundType(): Int =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE
        } else {
            0
        }

    companion object {
        const val ACTION_STOP = "io.pagecrawl.relay.STOP"
        const val ACTION_KEY_CHANGED = "io.pagecrawl.relay.KEY_CHANGED"
        const val ACTION_SETTINGS_CHANGED = "io.pagecrawl.relay.SETTINGS_CHANGED"

        private const val CHANNEL_ID = "relay"
        private const val NOTIFICATION_ID = 1
        private const val STATUS_INTERVAL_MS = 5_000L

        /** Starts the service, or tells a running one that [action] happened. */
        fun start(context: Context, action: String? = null) {
            val intent = Intent(context, RelayService::class.java).setAction(action)
            ContextCompat.startForegroundService(context, intent)
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, RelayService::class.java))
        }

        /** The platform the gateway sees, in Go's own words (android/arm64, android/amd64). */
        fun platform(): String {
            val arch = when (Build.SUPPORTED_ABIS.firstOrNull()) {
                "arm64-v8a" -> "arm64"
                "x86_64" -> "amd64"
                "armeabi-v7a" -> "arm"
                "x86" -> "386"
                else -> Build.SUPPORTED_ABIS.firstOrNull() ?: "unknown"
            }
            return "android/$arch"
        }

        fun network(caps: NetworkCapabilities?): NetworkPolicy.Network? = caps?.let {
            NetworkPolicy.Network(
                hasInternet = it.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET),
                isWifiOrEthernet = it.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) ||
                    it.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET),
                isMetered = !it.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED),
            )
        }

        fun headline(decision: Decision?, status: RelayStatus): String = when {
            decision == Decision.WAITING_FOR_WIFI -> "Waiting for Wi-Fi"
            decision == Decision.NO_NETWORK -> "No connection"
            status.connected -> "Relaying checks"
            status.rejected -> "Key not accepted"
            decision == Decision.RUN && status.lastError.isNotEmpty() -> "Could not connect"
            else -> "Connecting"
        }

        fun detail(decision: Decision?, status: RelayStatus): String = when {
            decision == Decision.WAITING_FOR_WIFI ->
                "On mobile data. The relay resumes on Wi-Fi."
            decision == Decision.NO_NETWORK -> "The relay resumes when the phone is back online."
            status.connected ->
                "${status.connections} ${if (status.connections == 1) "connection" else "connections"} carried, ${status.traffic}"
            // Said plainly, because retrying never fixes it: the key was revoked, the relay
            // was removed in PageCrawl, or the key was mistyped.
            status.rejected ->
                "PageCrawl did not accept this key. Remove it here and add the relay again under Settings, Relays."
            status.lastError.isNotEmpty() -> "Retrying. ${status.lastError}"
            else -> "Connecting to PageCrawl."
        }
    }
}
