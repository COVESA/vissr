package com.vissr.safetyscore.ui

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.ui.Modifier
import com.vissr.safetyscore.ui.theme.VissApiGrpcDemoTheme
import com.vissr.safetyscore.ui.view.CurveLoggingDataGraph
import com.vissr.safetyscore.ui.view.SafetyScore
import com.vissr.safetyscore.ui.view.SignalDataPoints
import org.koin.androidx.viewmodel.ext.android.viewModel

class MainActivity : ComponentActivity() {

    private val viewModel: VissApiGrpcDemoViewModel by viewModel()

    @OptIn(ExperimentalFoundationApi::class)
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        lifecycle.addObserver(viewModel)

        setContent {
            VissApiGrpcDemoTheme {
                // A surface container using the 'background' color from the theme
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = MaterialTheme.colorScheme.background
                ) {
                    val pagerState = rememberPagerState(pageCount = { 3 })
                    HorizontalPager(
                        state = pagerState,
                        modifier = Modifier.fillMaxSize(),
                    ) {
                        when (it) {
                            0 -> SignalDataPoints(viewModel = viewModel)
                            1 -> SafetyScore()
                            2 -> CurveLoggingDataGraph()
                        }
                    }
                }
            }
        }
    }
}

