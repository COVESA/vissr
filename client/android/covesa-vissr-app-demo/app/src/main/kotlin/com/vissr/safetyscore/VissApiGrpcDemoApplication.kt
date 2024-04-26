package com.vissr.safetyscore

import android.app.Application
import com.vissr.safetyscore.data.vissApiDataModule
import com.vissr.safetyscore.ui.vissApiGrpcDemoKoinModule
import org.koin.android.ext.koin.androidContext
import org.koin.android.ext.koin.androidLogger
import org.koin.core.context.startKoin
import org.koin.core.logger.Level

class VissApiGrpcDemoApplication: Application() {
    override fun onCreate() {
        super.onCreate()

        startKoin {
            androidLogger(Level.ERROR)
            androidContext(this@VissApiGrpcDemoApplication)
            modules(
                vissApiGrpcDemoKoinModule +
                    vissApiDataModule
            )
        }
    }
}
