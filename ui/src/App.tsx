import { useState } from 'react'
import { Dashboard } from './components/Dashboard'
import { History } from './components/History'
import './App.css'

type Page = 'dashboard' | 'history';

function App() {
  const [currentPage, setCurrentPage] = useState<Page>('dashboard');

  return (
    <div className="app">
      {currentPage === 'dashboard' && (
        <Dashboard onNavigateToHistory={() => setCurrentPage('history')} />
      )}
      {currentPage === 'history' && (
        <History onBack={() => setCurrentPage('dashboard')} />
      )}
    </div>
  )
}

export default App
