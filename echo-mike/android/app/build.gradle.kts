plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// CI passes the run number so every build installs over the last one.
val buildNumber = System.getenv("GITHUB_RUN_NUMBER")?.toIntOrNull() ?: 1

android {
    namespace = "com.credence.echomike"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.credence.echomike"
        // Android 10: saving attachments to Downloads without a storage
        // permission needs it.
        minSdk = 29
        targetSdk = 35
        versionCode = buildNumber
        versionName = "1.2.$buildNumber"
    }

    signingConfigs {
        // Sideload key kept in the repo so every build can update the last
        // one, like CPC Mobile. Replace it before publishing on Google Play.
        create("sideload") {
            storeFile = file("echomike-sideload.jks")
            storePassword = System.getenv("ECHOMIKE_KEYSTORE_PASSWORD") ?: "echomike-sideload"
            keyAlias = "echomike"
            keyPassword = System.getenv("ECHOMIKE_KEYSTORE_PASSWORD") ?: "echomike-sideload"
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = signingConfigs.getByName("sideload")
        }
        debug {
            signingConfig = signingConfigs.getByName("sideload")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    // The Go mail engine, built by `gomobile bind` (see build.sh).
    implementation(files("libs/echomike.aar"))
}
