plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

// The release workflow passes the tag (v1.2.3) as -PrelayVersion=1.2.3. A local build is
// "0.0.0-dev". The version code is derived from it so every release installs over the last.
val relayVersion = (findProperty("relayVersion") as String?) ?: "0.0.0-dev"
val relayVersionCode = relayVersion.substringBefore("-").split(".").map { it.toIntOrNull() ?: 0 }
    .let { (it + listOf(0, 0, 0)).take(3) }
    .let { (major, minor, patch) -> major * 10_000 + minor * 100 + patch }
    .coerceAtLeast(1)

// A published APK must be signed with the one release key, forever: Android refuses to install
// an update signed with a different key. The key comes from the environment (the release
// workflow decodes it from repository secrets). Without it a release build uses the local
// debug key, which is fine for a test device and is never published.
val releaseKeystore = System.getenv("RELAY_KEYSTORE_FILE")?.let(::file)?.takeIf { it.exists() }

android {
    namespace = "io.pagecrawl.relay"
    compileSdk = 36

    defaultConfig {
        // Its own application id: the relay is a separate app from the PageCrawl app
        // (io.pagecrawl.app), and installs and updates independently of it.
        applicationId = "io.pagecrawl.relay"
        minSdk = 26
        targetSdk = 36
        versionCode = relayVersionCode
        versionName = relayVersion
    }

    signingConfigs {
        if (releaseKeystore != null) {
            create("release") {
                storeFile = releaseKeystore
                storePassword = System.getenv("RELAY_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("RELAY_KEY_ALIAS")
                keyPassword = System.getenv("RELAY_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.findByName("release") ?: signingConfigs.getByName("debug")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }
}

dependencies {
    // The relay itself, built from ../mobile with mobile/build-aar.sh.
    implementation(files("libs/relay.aar"))

    implementation(platform("androidx.compose:compose-bom:2025.05.01"))
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.activity:activity-compose:1.10.1")
    implementation("androidx.core:core-ktx:1.16.0")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.9.3")

    // QR scanning without Google Play services, so it works on any Android phone the app
    // is sideloaded onto.
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")

    testImplementation("junit:junit:4.13.2")
    // Real org.json for JVM tests; Android's copy is a stub off-device.
    testImplementation("org.json:json:20240303")
}
