package com.credence.echomike

import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context
import android.util.Log
import mobile.Mobile

/**
 * Checks for new mail about every 15 minutes, the shortest Android allows,
 * even when the app isn't open. Android may stretch that in battery saver.
 */
class MailCheckJob : JobService() {

    override fun onStartJob(params: JobParameters): Boolean {
        Echo.init(this)
        val dir = Echo.dataDir(this)
        Thread {
            try {
                Mobile.checkMail(dir, Echo)
            } catch (e: Exception) {
                Log.w("EchoMike", "mail check failed", e)
            } finally {
                jobFinished(params, false)
            }
        }.start()
        return true
    }

    override fun onStopJob(params: JobParameters): Boolean = true

    companion object {
        private const val JOB_ID = 1

        fun schedule(context: Context) {
            // Background checks are a bonus; never let them stop the app
            // from opening.
            try {
                val js = context.getSystemService(JobScheduler::class.java)
                if (js.getPendingJob(JOB_ID) != null) return
                js.schedule(
                    JobInfo.Builder(JOB_ID, ComponentName(context, MailCheckJob::class.java))
                        .setPeriodic(15 * 60 * 1000L)
                        .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                        .setPersisted(true)
                        .build()
                )
            } catch (e: Exception) {
                Log.w("EchoMike", "couldn't schedule background mail checks", e)
            }
        }
    }
}
