# PageCrawl Relay for Android Privacy Policy

Effective date: October 11, 2026

Mygtukynas MB ("we", "us", or "our") publishes PageCrawl Relay for Android
("Relay"). This policy explains what Relay accesses, collects, uses and shares,
and how to remove its data. Contact us at
[support@pagecrawl.io](mailto:support@pagecrawl.io) with privacy questions.

Relay lets PageCrawl use your phone's internet connection to check public websites
for monitors your team chooses to send through it. PageCrawl checks the pages,
saves results and sends alerts; your phone provides the connection. Relay does
not sign in to a PageCrawl account and has no monitors or alerts of its own. Your
PageCrawl account and monitoring content are also covered by the
[PageCrawl Privacy Policy](https://pagecrawl.io/privacy-policy).

## What Relay keeps on your phone

- Your relay key, which you scan from PageCrawl or paste into Relay. It is stored
  encrypted using a key held by Android's Keystore. Relay disables Android cloud
  backup.
- Whether Relay checks are enabled and whether Relay may use mobile data.
- If you use optional Assistant mode: the reconnect interval, idle wait,
  airplane-mode pause, whether scheduled reconnects are enabled and the next
  scheduled attempt.
- Recovery information for an interrupted reconnect, including whether airplane
  mode needs to be restored and the identifier and expiry of PageCrawl's temporary
  reconnect pause. This helps Relay recover after an interruption or restart.
- Current status, recent destination sites, self-check results and public IP
  comparisons displayed in the app. These are held in memory, not saved to app
  storage. Recent destination history is cleared when Relay checks are switched
  off. A few diagnostic lines, such as a blocked destination, go to Android's
  system log on the phone.

The camera is used only to scan the relay-key QR code. Relay does not save or
upload camera images. It checks Wi-Fi and mobile-network availability on the phone
to follow your mobile-data choice and to manage reconnects.

## What Relay sends to PageCrawl

When it connects, Relay sends the relay key to identify and authenticate the relay,
its platform (Android and processor type) and app version. PageCrawl also receives
the phone's public IP address through the connection. Relay does not send a device
name, model, hardware or advertising identifier, GPS location, contacts or the
recent-destination list shown in the app. The relay's name in PageCrawl is the name
you enter when adding it.

Before an Assistant-mode reconnect that you start or schedule, Relay asks
PageCrawl to pause new checks briefly. It sends the relay key and requested pause
duration. When mobile internet returns, it sends the key and pause identifier to
end the pause. These requests use HTTPS. If confirmation cannot be completed, the
temporary pause expires within five minutes.

PageCrawl stores the relay's name, platform, version, status, last-seen time,
most recent public IP, any monthly data limit you set, key expiry, and daily totals
of data carried, connections and checks served. It stores a one-way hash of the
relay key rather than the key itself. It also stores the most recent reconnect
pause's identifier, expiry and completion time with the relay record. Team members
can see relay details under Settings, More, Relays. Connection logs record the
connecting IP address and connection time.

We use this information to operate Relay, authenticate it, apply your settings
and limits, coordinate reconnects, and maintain the security of the service. We
do not sell it or use it for advertising. Our hosting providers process service
data on our behalf.

## What travels through your phone

Relay carries your team's checks and page previews that are set to use it. The
websites contacted see the phone's public IP address. For HTTPS websites, content
stays encrypted between PageCrawl and the website, so the phone sees the destination
sites but not their content. Plain HTTP traffic passes through the phone
unencrypted. Relay does not save the requests or responses it carries and refuses
local and private-network destinations.

## Other services Relay contacts

When you tap **Run self-check**, and before and after an Assistant-mode reconnect
you start or schedule, Relay contacts [ipify](https://www.ipify.org/) at
`api.ipify.org` over HTTPS to determine the phone's public IP address. ipify receives
that IP address. The IP addresses displayed in self-checks and reconnect comparisons
remain in app memory, not app storage. The reconnect comparison is not sent
separately to PageCrawl; PageCrawl observes the public IP used to connect to its
service.

Relay contains no advertising, analytics or crash-reporting software. It does not
record speech, access the microphone, read other apps' screens, take screenshots,
or access contacts, photos, messages or calendar data.

## Permissions and optional Assistant mode

- **Camera:** scan the QR code containing your relay key.
- **Notifications:** show Relay's ongoing status and interrupted-reconnect recovery.
- **Network access and network state:** carry checks, follow your mobile-data
  choice, perform self-checks and coordinate reconnects.
- **Foreground service:** continue the checks you switch on with an ongoing status
  notification.
- **Keeping the phone awake:** keep the processor and Wi-Fi available while
  relaying, and keep the processor awake during reconnect or recovery. While Relay
  is open for a reconnect or an enabled schedule, it can also keep the screen on.
- **Battery optimisation exemption:** requested only when you tap **Keep running
  in the background**, which opens Android's prompt.
- **Start at boot:** resume Relay if you left it enabled, and recover an
  interrupted reconnect after a restart or app update.

Assistant mode is optional and requires selecting Relay as the phone's default
digital assistant, replacing the previous assistant. It uses Android's airplane-mode
action only for reconnects you start or schedule and recovery of interrupted
reconnects. It does not listen for voice commands. Turn **Scheduled reconnects**
off to stop future scheduled attempts, and choose another assistant in Android
settings to remove Relay's assistant role.

## Retention and deletion

Switching **Relay checks** off stops relaying and closes its connections.
**Remove key from this phone** deletes the locally stored relay key. Clearing app
storage or uninstalling Relay removes its stored preferences and recovery data as
well. Data held only in app memory is lost when the app process ends.

The relay remains listed in PageCrawl until you remove it under Settings, More,
Relays. Removing it deletes its record, reconnect metadata and usage history, and
monitors set to use only that relay return to PageCrawl's own locations. Deleting
your PageCrawl account also removes the relays of teams you own. You can delete
your account at [pagecrawl.io/app/account/delete](https://pagecrawl.io/app/account/delete)
or through Account Settings on PageCrawl. A relay key stops working when it expires,
at most one year after it was added.

Deleting a relay does not erase existing monitoring results from your account.
Their retention follows your monitoring plan and the
[PageCrawl Privacy Policy](https://pagecrawl.io/privacy-policy). Service logs and
backups are retained separately under that policy. Deleted database data can remain
in the database provider's automated backups until it falls outside that provider's
backup window; it is not restored to the service.

## Children and policy changes

Relay is not directed at anyone under 18, and we do not knowingly collect data
from children. We update this policy and its effective date when our practices
change. Material changes are also announced by email or in the app before they
take effect.

For questions or requests concerning your data, email
[support@pagecrawl.io](mailto:support@pagecrawl.io).
