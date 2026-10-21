plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// CI passes the run number so every build installs over the last one.
val buildNumber = System.getenv("GITHUB_RUN_NUMBER")?.toIntOrNull() ?: 1

android {
    namespace = "com.credence.cpc"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.credence.cpc"
        minSdk = 26
        targetSdk = 35
        versionCode = buildNumber
        versionName = "1.0.$buildNumber"
    }

    signingConfigs {
        // Sideload key kept in the repo so every build can update the last
        // one. Replace it before publishing on Google Play. See README.
        create("sideload") {
            storeFile = file("cpc-sideload.jks")
            storePassword = System.getenv("CPC_KEYSTORE_PASSWORD") ?: "cpc-sideload"
            keyAlias = "cpc"
            keyPassword = System.getenv("CPC_KEYSTORE_PASSWORD") ?: "cpc-sideload"
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

    lint {
        // Special permissions (all files, usage access) are intentional for a
        // cleaner that is sideloaded, not published on Play.
        disable += setOf("ProtectedPermissions", "ScopedStorage", "QueryAllPackagesPermission")
    }
}

dependencies {
    testImplementation("junit:junit:4.13.2")
}
