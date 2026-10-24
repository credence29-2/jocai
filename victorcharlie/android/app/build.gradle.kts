plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// CI passes the run number so every build installs over the last one.
val buildNumber = System.getenv("GITHUB_RUN_NUMBER")?.toIntOrNull() ?: 1

android {
    namespace = "com.credence.victorcharlie"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.credence.victorcharlie"
        minSdk = 26
        targetSdk = 35
        versionCode = buildNumber
        versionName = "0.1.$buildNumber"
    }

    signingConfigs {
        // Sideload key kept in the repo so every build can update the last
        // one. Replace it before publishing on Google Play.
        create("sideload") {
            storeFile = file("vc-sideload.jks")
            storePassword = System.getenv("VC_KEYSTORE_PASSWORD") ?: "vc-sideload"
            keyAlias = "vc"
            keyPassword = System.getenv("VC_KEYSTORE_PASSWORD") ?: "vc-sideload"
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
